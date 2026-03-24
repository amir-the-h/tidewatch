package inference

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	tokenizers "github.com/daulet/tokenizers"
	ort "github.com/yalue/onnxruntime_go"
)

const maxSeqLen = 512

// modelConfig is the subset of a HuggingFace config.json we need.
type modelConfig struct {
	ModelType     string            `json:"model_type"`
	Architectures []string          `json:"architectures"`
	ID2Label      map[string]string `json:"id2label"`
}

// ONNXPipeline implements Pipeline using onnxruntime_go and daulet/tokenizers.
// Requires: InitRuntime called once before Load, CGO enabled, Rust toolchain,
// ONNX Runtime 1.24.1 shared library with CUDA execution provider.
type ONNXPipeline struct {
	name      string
	modelPath string

	tokenizer *tokenizers.Tokenizer
	session   *ort.AdvancedSession

	id2label      []string // indexed by logit position
	numLabels     int
	isNER         bool // true for token-classification (per-token logits)
	isZeroShot    bool // true for NLI models used as zero-shot classifiers
	entailmentIdx int  // index of "entailment" class in id2label
	hasTypeIDs    bool // true for DeBERTa-v2/v3 models

	// candidate labels for zero-shot (guarded by labelsMu)
	labelsMu sync.RWMutex
	labels   []string

	// pre-allocated backing slices — modified in-place per inference
	inputIDsData []int64
	attnMaskData []int64
	typeIDsData  []int64 // nil if !hasTypeIDs

	// pre-allocated tensors wrapping the backing slices
	inputIDsTensor *ort.Tensor[int64]
	attnMaskTensor *ort.Tensor[int64]
	typeIDsTensor  *ort.Tensor[int64] // nil if !hasTypeIDs
	logitsTensor   *ort.Tensor[float32]

	mu sync.Mutex // guards tensor data + session.Run
}

// NewONNXPipeline creates a new ONNXPipeline with the given model name.
func NewONNXPipeline(name string) *ONNXPipeline {
	return &ONNXPipeline{name: name}
}

// Name returns the model name as registered in the models table.
func (p *ONNXPipeline) Name() string { return p.name }

// SetLabels updates the candidate labels used by zero-shot classification.
// Safe to call concurrently with Run.
func (p *ONNXPipeline) SetLabels(labels []string) {
	cp := make([]string, len(labels))
	copy(cp, labels)
	p.labelsMu.Lock()
	p.labels = cp
	p.labelsMu.Unlock()
}

// Load initializes the model from the given local .onnx file path.
// The tokenizer.json and config.json must be in the same directory.
func (p *ONNXPipeline) Load(modelPath string) error {
	p.modelPath = modelPath
	dir := filepath.Dir(modelPath)

	// 1. Load tokenizer — may be in dir or one level up (e.g. when the .onnx
	//    file lives in an onnx/ subdirectory as with hf download output).
	tokenizerDir := dir
	if _, statErr := os.Stat(filepath.Join(dir, "tokenizer.json")); os.IsNotExist(statErr) {
		tokenizerDir = filepath.Dir(dir)
	}
	tok, err := tokenizers.FromFile(filepath.Join(tokenizerDir, "tokenizer.json"))
	if err != nil {
		return fmt.Errorf("inference: onnx: load tokenizer: %w", err)
	}
	p.tokenizer = tok

	// 2. Parse config.json for label map and model family.
	cfg, err := loadModelConfig(tokenizerDir)
	if err != nil {
		return fmt.Errorf("inference: onnx: load config: %w", err)
	}

	// 3. Derive features from the config.
	// Detect token_type_ids by scanning the ONNX file directly — more reliable
	// than inferring from model_type, since pre-exported models may omit it.
	p.hasTypeIDs = onnxHasInput(modelPath, "token_type_ids")
	for _, arch := range cfg.Architectures {
		if strings.Contains(arch, "TokenClassification") {
			p.isNER = true
			break
		}
	}
	p.numLabels = len(cfg.ID2Label)
	p.id2label = make([]string, p.numLabels)
	for k, v := range cfg.ID2Label {
		idx, err := strconv.Atoi(k)
		if err != nil || idx < 0 || idx >= p.numLabels {
			continue
		}
		p.id2label[idx] = v
		if strings.ToLower(v) == "entailment" {
			p.isZeroShot = true
			p.entailmentIdx = idx
		}
	}

	// 4. Pre-allocate input backing slices and tensors.
	p.inputIDsData = make([]int64, maxSeqLen)
	p.attnMaskData = make([]int64, maxSeqLen)
	inputShape := ort.NewShape(1, int64(maxSeqLen))

	p.inputIDsTensor, err = ort.NewTensor(inputShape, p.inputIDsData)
	if err != nil {
		return fmt.Errorf("inference: onnx: input_ids tensor: %w", err)
	}
	p.attnMaskTensor, err = ort.NewTensor(inputShape, p.attnMaskData)
	if err != nil {
		return fmt.Errorf("inference: onnx: attention_mask tensor: %w", err)
	}

	inputs := []ort.Value{p.inputIDsTensor, p.attnMaskTensor}
	inputNames := []string{"input_ids", "attention_mask"}

	if p.hasTypeIDs {
		p.typeIDsData = make([]int64, maxSeqLen)
		p.typeIDsTensor, err = ort.NewTensor(inputShape, p.typeIDsData)
		if err != nil {
			return fmt.Errorf("inference: onnx: token_type_ids tensor: %w", err)
		}
		inputs = append(inputs, p.typeIDsTensor)
		inputNames = append(inputNames, "token_type_ids")
	}

	// 5. Pre-allocate output tensor.
	//    Sequence classification: [1, numLabels]
	//    Token classification:    [1, maxSeqLen, numLabels]
	var logitsShape ort.Shape
	var logitsData []float32
	if p.isNER {
		logitsShape = ort.NewShape(1, int64(maxSeqLen), int64(p.numLabels))
		logitsData = make([]float32, maxSeqLen*p.numLabels)
	} else {
		logitsShape = ort.NewShape(1, int64(p.numLabels))
		logitsData = make([]float32, p.numLabels)
	}
	p.logitsTensor, err = ort.NewTensor(logitsShape, logitsData)
	if err != nil {
		return fmt.Errorf("inference: onnx: logits tensor: %w", err)
	}

	// 6. Configure session options; append CUDA EP with conservative arena
	//    settings to avoid large GPU pre-allocations. If CUDA is unavailable
	//    or fails to initialise, fall through to CPU-only.
	sessionOpts, err := ort.NewSessionOptions()
	if err != nil {
		return fmt.Errorf("inference: onnx: session options: %w", err)
	}
	defer sessionOpts.Destroy()

	cudaAppended := false
	cudaOpts, cudaErr := ort.NewCUDAProviderOptions()
	if cudaErr == nil {
		cudaErr = cudaOpts.Update(map[string]string{
			"device_id":             "0",
			"arena_extend_strategy": "kSameAsRequested", // avoid large pre-allocation
		})
		if cudaErr == nil {
			cudaErr = sessionOpts.AppendExecutionProviderCUDA(cudaOpts)
			if cudaErr == nil {
				cudaAppended = true
			}
		}
		cudaOpts.Destroy()
	}
	if cudaErr != nil {
		slog.Warn("inference: onnx: CUDA EP unavailable, using CPU",
			"model", p.name, "error", cudaErr)
	}

	// 7. Create the ONNX session. If CUDA was appended but session init fails
	//    (e.g. GPU OOM), retry with a fresh CPU-only session options.
	p.session, err = ort.NewAdvancedSession(
		modelPath,
		inputNames, []string{"logits"},
		inputs, []ort.Value{p.logitsTensor},
		sessionOpts,
	)
	if err != nil && cudaAppended {
		slog.Warn("inference: onnx: CUDA session init failed, retrying on CPU",
			"model", p.name, "error", err)
		cpuOpts, optErr := ort.NewSessionOptions()
		if optErr != nil {
			return fmt.Errorf("inference: onnx: cpu session options: %w", optErr)
		}
		defer cpuOpts.Destroy()
		p.session, err = ort.NewAdvancedSession(
			modelPath,
			inputNames, []string{"logits"},
			inputs, []ort.Value{p.logitsTensor},
			cpuOpts,
		)
	}
	if err != nil {
		return fmt.Errorf("inference: onnx: create session: %w", err)
	}

	return nil
}

// Run tokenizes text, runs inference on GPU, and returns a JSON result.
func (p *ONNXPipeline) Run(text []byte) (json.RawMessage, error) {
	if p.session == nil {
		return nil, fmt.Errorf("inference: onnx: model not loaded")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isZeroShot {
		return p.runZeroShot(text)
	}

	// Tokenize with special tokens ([CLS]/[SEP]) and attention mask.
	enc := p.tokenizer.EncodeWithOptions(
		string(text), true,
		tokenizers.WithReturnAttentionMask(),
		tokenizers.WithReturnTokens(),
	)

	ids := enc.IDs
	mask := enc.AttentionMask
	tokens := enc.Tokens
	seqLen := len(ids)
	if seqLen > maxSeqLen {
		ids = ids[:maxSeqLen]
		mask = mask[:maxSeqLen]
		tokens = tokens[:maxSeqLen]
		seqLen = maxSeqLen
	}

	p.fillInputs(ids, mask)

	if err := p.session.Run(); err != nil {
		return nil, fmt.Errorf("inference: onnx: run: %w", err)
	}

	logits := p.logitsTensor.GetData()
	if p.isNER {
		return postProcessNER(logits, p.id2label, p.numLabels, seqLen, tokens)
	}
	return postProcessClassification(logits, p.id2label)
}

// runZeroShot performs zero-shot classification using NLI: for each candidate
// label, it encodes (text, hypothesis) as a pair, runs inference, and collects
// the entailment score. Called with p.mu already held.
func (p *ONNXPipeline) runZeroShot(text []byte) (json.RawMessage, error) {
	p.labelsMu.RLock()
	labels := make([]string, len(p.labels))
	copy(labels, p.labels)
	p.labelsMu.RUnlock()

	if len(labels) == 0 {
		return json.Marshal(zeroShotResult{Labels: []string{}, Scores: []float32{}})
	}

	entailmentLogits := make([]float32, len(labels))
	for i, label := range labels {
		hypothesis := "This text is about " + label + "."
		// Concatenate with [SEP] to form a pair: [CLS] text [SEP] hypothesis [SEP]
		input := string(text) + " [SEP] " + hypothesis

		enc := p.tokenizer.EncodeWithOptions(
			input, true,
			tokenizers.WithReturnAttentionMask(),
		)

		ids := enc.IDs
		mask := enc.AttentionMask
		if len(ids) > maxSeqLen {
			ids = ids[:maxSeqLen]
			mask = mask[:maxSeqLen]
		}

		p.fillInputs(ids, mask)

		if err := p.session.Run(); err != nil {
			return nil, fmt.Errorf("inference: onnx: zero-shot [%s]: %w", label, err)
		}

		logits := p.logitsTensor.GetData()
		probs := softmax(logits)
		entailmentLogits[i] = probs[p.entailmentIdx]
	}

	// Normalize entailment scores across all labels.
	scores := softmax(entailmentLogits)

	type pair struct {
		label string
		score float32
	}
	pairs := make([]pair, len(labels))
	for i := range labels {
		pairs[i] = pair{labels[i], scores[i]}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].score > pairs[j].score })

	sortedLabels := make([]string, len(pairs))
	sortedScores := make([]float32, len(pairs))
	for i, kv := range pairs {
		sortedLabels[i] = kv.label
		sortedScores[i] = kv.score
	}
	return json.Marshal(zeroShotResult{Labels: sortedLabels, Scores: sortedScores})
}

// fillInputs writes ids and mask into the pre-allocated backing slices.
// Called with p.mu held.
func (p *ONNXPipeline) fillInputs(ids, mask []uint32) {
	for i := range p.inputIDsData {
		p.inputIDsData[i] = 0
		p.attnMaskData[i] = 0
	}
	for i, id := range ids {
		p.inputIDsData[i] = int64(id)
		p.attnMaskData[i] = int64(mask[i])
	}
	if p.hasTypeIDs {
		for i := range p.typeIDsData {
			p.typeIDsData[i] = 0
		}
	}
}

// RunBatch runs inference on multiple texts, looping over Run().
func (p *ONNXPipeline) RunBatch(texts [][]byte) ([]json.RawMessage, error) {
	results := make([]json.RawMessage, len(texts))
	for i, t := range texts {
		r, err := p.Run(t)
		if err != nil {
			return nil, err
		}
		results[i] = r
	}
	return results, nil
}

// Close releases all ONNX Runtime and tokenizer resources.
func (p *ONNXPipeline) Close() error {
	if p.session != nil {
		if err := p.session.Destroy(); err != nil {
			return fmt.Errorf("inference: onnx: destroy session: %w", err)
		}
		p.session = nil
	}
	if p.tokenizer != nil {
		if err := p.tokenizer.Close(); err != nil {
			return fmt.Errorf("inference: onnx: close tokenizer: %w", err)
		}
		p.tokenizer = nil
	}
	for _, t := range []*ort.Tensor[int64]{p.inputIDsTensor, p.attnMaskTensor, p.typeIDsTensor} {
		if t != nil {
			_ = t.Destroy()
		}
	}
	p.inputIDsTensor, p.attnMaskTensor, p.typeIDsTensor = nil, nil, nil
	if p.logitsTensor != nil {
		_ = p.logitsTensor.Destroy()
		p.logitsTensor = nil
	}
	return nil
}

// ── post-processing ───────────────────────────────────────────────────────────

type classificationResult struct {
	Label string  `json:"label"`
	Score float32 `json:"score"`
}

type zeroShotResult struct {
	Labels []string  `json:"labels"`
	Scores []float32 `json:"scores"`
}

type nerResult struct {
	Entities []nerEntity `json:"entities"`
}

type nerEntity struct {
	Word  string  `json:"word"`
	Label string  `json:"label"`
	Score float32 `json:"score"`
}

func postProcessClassification(logits []float32, id2label []string) (json.RawMessage, error) {
	probs := softmax(logits)
	best := 0
	for i, v := range probs {
		if v > probs[best] {
			best = i
		}
	}
	label := ""
	if best < len(id2label) {
		label = id2label[best]
	}
	return json.Marshal(classificationResult{Label: label, Score: probs[best]})
}

// postProcessNER extracts non-"O" token labels, merges consecutive same-label
// tokens into word spans, skipping [CLS] (position 0) and [SEP] (last position).
func postProcessNER(logits []float32, id2label []string, numLabels, seqLen int, tokens []string) (json.RawMessage, error) {
	type span struct {
		label  string
		tokens []string
		scores []float32
	}

	var spans []span
	var cur *span

	flush := func() {
		if cur != nil {
			spans = append(spans, *cur)
			cur = nil
		}
	}

	// Position 0 is [CLS]; seqLen-1 is [SEP] — skip both.
	for i := 1; i < seqLen-1; i++ {
		tokenLogits := logits[i*numLabels : (i+1)*numLabels]
		probs := softmax(tokenLogits)
		best := 0
		for j, v := range probs {
			if v > probs[best] {
				best = j
			}
		}
		label := ""
		if best < len(id2label) {
			label = id2label[best]
		}

		tok := ""
		if i < len(tokens) {
			tok = tokens[i]
		}

		if label == "O" {
			flush()
			continue
		}
		if cur == nil || cur.label != label {
			flush()
			cur = &span{label: label}
		}
		cur.tokens = append(cur.tokens, tok)
		cur.scores = append(cur.scores, probs[best])
	}
	flush()

	entities := make([]nerEntity, 0, len(spans))
	for _, s := range spans {
		var avgScore float32
		for _, sc := range s.scores {
			avgScore += sc
		}
		avgScore /= float32(len(s.scores))
		entities = append(entities, nerEntity{
			Word:  joinTokens(s.tokens),
			Label: s.label,
			Score: avgScore,
		})
	}
	return json.Marshal(nerResult{Entities: entities})
}

// joinTokens reconstructs a word from subword tokens.
// Handles both RoBERTa-style BPE (Ġ prefix = space) and SentencePiece (▁ prefix = word start).
func joinTokens(toks []string) string {
	var sb strings.Builder
	for _, t := range toks {
		if strings.HasPrefix(t, "Ġ") {
			if sb.Len() > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(t[len("Ġ"):])
		} else if strings.HasPrefix(t, "▁") {
			if sb.Len() > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(t[len("▁"):])
		} else {
			sb.WriteString(t)
		}
	}
	return strings.TrimSpace(sb.String())
}

func softmax(logits []float32) []float32 {
	if len(logits) == 0 {
		return nil
	}
	max := logits[0]
	for _, v := range logits[1:] {
		if v > max {
			max = v
		}
	}
	out := make([]float32, len(logits))
	var sum float64
	for i, v := range logits {
		e := math.Exp(float64(v) - float64(max))
		out[i] = float32(e)
		sum += e
	}
	for i := range out {
		out[i] = float32(float64(out[i]) / sum)
	}
	return out
}

// ── onnx introspection ───────────────────────────────────────────────────────

// onnxHasInput reports whether the ONNX model at path lists inputName as a
// graph input. It reads only the first 16 KB of the file (input names are
// stored near the beginning of the protobuf) to avoid loading the full model.
func onnxHasInput(path, inputName string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 16*1024)
	n, _ := f.Read(buf)
	return strings.Contains(string(buf[:n]), inputName)
}

// ── config loading ────────────────────────────────────────────────────────────

func loadModelConfig(dir string) (*modelConfig, error) {
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return nil, fmt.Errorf("read config.json: %w", err)
	}
	var cfg modelConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config.json: %w", err)
	}
	return &cfg, nil
}
