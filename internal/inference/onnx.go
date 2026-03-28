package inference

import (
	"context"
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

// ── shared session cache ──────────────────────────────────────────────────────

var (
	sessionCacheMu sync.Mutex
	sessionCache   = make(map[string]*sharedONNXSession)
)

// sharedONNXSession holds a single loaded ONNX session shared across all
// ONNXPipeline instances that reference the same model file. Inference calls
// are serialized through mu so that pre-allocated tensors are not clobbered
// by concurrent pipelines.
type sharedONNXSession struct {
	path      string
	tokenizer *tokenizers.Tokenizer
	session   *ort.AdvancedSession

	// Immutable metadata derived from config.json and the ONNX graph.
	id2label      []string
	numLabels     int
	isNER         bool
	isZeroShot    bool
	entailmentIdx int
	hasTypeIDs    bool

	// Pre-allocated tensors bound to session at creation time.
	inputIDsData   []int64
	attnMaskData   []int64
	typeIDsData    []int64 // nil if !hasTypeIDs
	inputIDsTensor *ort.Tensor[int64]
	attnMaskTensor *ort.Tensor[int64]
	typeIDsTensor  *ort.Tensor[int64] // nil if !hasTypeIDs
	logitsTensor   *ort.Tensor[float32]

	mu       sync.Mutex // serialises all session.Run calls across shared pipelines
	refCount int        // guarded by sessionCacheMu
}

// fillInputs writes ids and mask into the pre-allocated backing slices.
// Must be called with s.mu held.
func (s *sharedONNXSession) fillInputs(ids, mask []uint32) {
	for i := range s.inputIDsData {
		s.inputIDsData[i] = 0
		s.attnMaskData[i] = 0
	}
	for i, id := range ids {
		s.inputIDsData[i] = int64(id)
		s.attnMaskData[i] = int64(mask[i])
	}
	if s.hasTypeIDs {
		for i := range s.typeIDsData {
			s.typeIDsData[i] = 0
		}
	}
}

// closeResources destroys the ORT session, tokenizer, and tensors.
// Called only when refCount reaches zero.
func (s *sharedONNXSession) closeResources() error {
	var firstErr error
	if s.session != nil {
		if err := s.session.Destroy(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("inference: onnx: destroy session: %w", err)
		}
		s.session = nil
	}
	if s.tokenizer != nil {
		if err := s.tokenizer.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("inference: onnx: close tokenizer: %w", err)
		}
		s.tokenizer = nil
	}
	for _, t := range []*ort.Tensor[int64]{s.inputIDsTensor, s.attnMaskTensor, s.typeIDsTensor} {
		if t != nil {
			_ = t.Destroy()
		}
	}
	s.inputIDsTensor, s.attnMaskTensor, s.typeIDsTensor = nil, nil, nil
	if s.logitsTensor != nil {
		_ = s.logitsTensor.Destroy()
		s.logitsTensor = nil
	}
	return firstErr
}

// acquireSession returns the cached sharedONNXSession for modelPath, loading
// it on first use. Callers must call releaseSession when done.
func acquireSession(modelPath string) (*sharedONNXSession, error) {
	sessionCacheMu.Lock()
	if sess, ok := sessionCache[modelPath]; ok {
		sess.refCount++
		sessionCacheMu.Unlock()
		return sess, nil
	}
	sessionCacheMu.Unlock()

	// Load outside the lock — this is the slow path (ONNX session creation).
	sess, err := loadSharedSession(modelPath)
	if err != nil {
		return nil, err
	}

	sessionCacheMu.Lock()
	// Another goroutine may have loaded the same path concurrently; if so,
	// discard our copy and use theirs.
	if existing, ok := sessionCache[modelPath]; ok {
		existing.refCount++
		sessionCacheMu.Unlock()
		_ = sess.closeResources()
		return existing, nil
	}
	sess.refCount = 1
	sessionCache[modelPath] = sess
	sessionCacheMu.Unlock()
	return sess, nil
}

// releaseSession decrements the ref count. The session is destroyed when it
// reaches zero.
func releaseSession(sess *sharedONNXSession) error {
	sessionCacheMu.Lock()
	sess.refCount--
	destroy := sess.refCount == 0
	if destroy {
		delete(sessionCache, sess.path)
	}
	sessionCacheMu.Unlock()
	if destroy {
		return sess.closeResources()
	}
	return nil
}

// loadSharedSession creates a brand-new sharedONNXSession from modelPath.
func loadSharedSession(modelPath string) (*sharedONNXSession, error) {
	s := &sharedONNXSession{path: modelPath}
	dir := filepath.Dir(modelPath)

	// 1. Tokenizer — may be in dir or one level up (e.g. onnx/ sub-dir).
	tokenizerDir := dir
	if _, statErr := os.Stat(filepath.Join(dir, "tokenizer.json")); os.IsNotExist(statErr) {
		tokenizerDir = filepath.Dir(dir)
	}
	tok, err := tokenizers.FromFile(filepath.Join(tokenizerDir, "tokenizer.json"))
	if err != nil {
		return nil, fmt.Errorf("inference: onnx: load tokenizer: %w", err)
	}
	s.tokenizer = tok

	// 2. Parse config.json for label map and model family.
	cfg, err := loadModelConfig(tokenizerDir)
	if err != nil {
		_ = s.tokenizer.Close()
		return nil, fmt.Errorf("inference: onnx: load config: %w", err)
	}

	// 3. Derive features from the config.
	s.hasTypeIDs = onnxHasInput(modelPath, "token_type_ids")
	for _, arch := range cfg.Architectures {
		if strings.Contains(arch, "TokenClassification") {
			s.isNER = true
			break
		}
	}
	s.numLabels = len(cfg.ID2Label)
	s.id2label = make([]string, s.numLabels)
	for k, v := range cfg.ID2Label {
		idx, idxErr := strconv.Atoi(k)
		if idxErr != nil || idx < 0 || idx >= s.numLabels {
			continue
		}
		s.id2label[idx] = v
		if strings.ToLower(v) == "entailment" {
			s.isZeroShot = true
			s.entailmentIdx = idx
		}
	}

	// 4. Pre-allocate input tensors.
	s.inputIDsData = make([]int64, maxSeqLen)
	s.attnMaskData = make([]int64, maxSeqLen)
	inputShape := ort.NewShape(1, int64(maxSeqLen))

	s.inputIDsTensor, err = ort.NewTensor(inputShape, s.inputIDsData)
	if err != nil {
		_ = s.tokenizer.Close()
		return nil, fmt.Errorf("inference: onnx: input_ids tensor: %w", err)
	}
	s.attnMaskTensor, err = ort.NewTensor(inputShape, s.attnMaskData)
	if err != nil {
		_ = s.tokenizer.Close()
		_ = s.inputIDsTensor.Destroy()
		return nil, fmt.Errorf("inference: onnx: attention_mask tensor: %w", err)
	}

	inputs := []ort.Value{s.inputIDsTensor, s.attnMaskTensor}
	inputNames := []string{"input_ids", "attention_mask"}

	if s.hasTypeIDs {
		s.typeIDsData = make([]int64, maxSeqLen)
		s.typeIDsTensor, err = ort.NewTensor(inputShape, s.typeIDsData)
		if err != nil {
			_ = s.tokenizer.Close()
			_ = s.inputIDsTensor.Destroy()
			_ = s.attnMaskTensor.Destroy()
			return nil, fmt.Errorf("inference: onnx: token_type_ids tensor: %w", err)
		}
		inputs = append(inputs, s.typeIDsTensor)
		inputNames = append(inputNames, "token_type_ids")
	}

	// 5. Pre-allocate output tensor.
	var logitsShape ort.Shape
	var logitsData []float32
	if s.isNER {
		logitsShape = ort.NewShape(1, int64(maxSeqLen), int64(s.numLabels))
		logitsData = make([]float32, maxSeqLen*s.numLabels)
	} else {
		logitsShape = ort.NewShape(1, int64(s.numLabels))
		logitsData = make([]float32, s.numLabels)
	}
	s.logitsTensor, err = ort.NewTensor(logitsShape, logitsData)
	if err != nil {
		_ = s.tokenizer.Close()
		_ = s.inputIDsTensor.Destroy()
		_ = s.attnMaskTensor.Destroy()
		if s.typeIDsTensor != nil {
			_ = s.typeIDsTensor.Destroy()
		}
		return nil, fmt.Errorf("inference: onnx: logits tensor: %w", err)
	}

	// 6. Configure session options; try CUDA EP first.
	sessionOpts, err := ort.NewSessionOptions()
	if err != nil {
		_ = s.closeResources()
		return nil, fmt.Errorf("inference: onnx: session options: %w", err)
	}
	defer sessionOpts.Destroy()

	cudaAppended := false
	cudaOpts, cudaErr := ort.NewCUDAProviderOptions()
	if cudaErr == nil {
		cudaErr = cudaOpts.Update(map[string]string{
			"device_id":             "0",
			"arena_extend_strategy": "kSameAsRequested",
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
			"path", modelPath, "error", cudaErr)
	}

	// 7. Create session; if CUDA OOM, retry with CPU-only options.
	s.session, err = ort.NewAdvancedSession(
		modelPath,
		inputNames, []string{"logits"},
		inputs, []ort.Value{s.logitsTensor},
		sessionOpts,
	)
	if err != nil && cudaAppended {
		slog.Warn("inference: onnx: CUDA session init failed, retrying on CPU",
			"path", modelPath, "error", err)
		cpuOpts, optErr := ort.NewSessionOptions()
		if optErr != nil {
			_ = s.closeResources()
			return nil, fmt.Errorf("inference: onnx: cpu session options: %w", optErr)
		}
		defer cpuOpts.Destroy()
		s.session, err = ort.NewAdvancedSession(
			modelPath,
			inputNames, []string{"logits"},
			inputs, []ort.Value{s.logitsTensor},
			cpuOpts,
		)
	}
	if err != nil {
		_ = s.closeResources()
		return nil, fmt.Errorf("inference: onnx: create session: %w", err)
	}

	return s, nil
}

// ── ONNXPipeline ──────────────────────────────────────────────────────────────

// ONNXPipeline implements Pipeline using onnxruntime_go and daulet/tokenizers.
// Multiple ONNXPipeline instances that point to the same .onnx file share a
// single underlying session via the package-level session cache, so the model
// weights are only loaded once per file regardless of how many model
// registrations reference it.
type ONNXPipeline struct {
	name   string
	shared *sharedONNXSession

	// Candidate labels for zero-shot classification (per-pipeline).
	labelsMu sync.RWMutex
	labels   []string
}

// NewONNXPipeline creates a new ONNXPipeline with the given model name.
func NewONNXPipeline(name string) *ONNXPipeline {
	return &ONNXPipeline{name: name}
}

// NewONNXPipelineFromConfig creates and loads an ONNX pipeline from DB pipeline config.
func NewONNXPipelineFromConfig(name string, config map[string]any) (*ONNXPipeline, error) {
	path, _ := config["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("inference: onnx: config missing 'path'")
	}
	p := &ONNXPipeline{name: name}
	if err := p.load(path); err != nil {
		return nil, err
	}
	return p, nil
}

// Name returns the model name as registered in the models table.
func (p *ONNXPipeline) Name() string { return p.name }

// Type returns the pipeline type.
func (p *ONNXPipeline) Type() PipelineType { return PipelineTypeONNX }

// SetLabels updates the candidate labels used by zero-shot classification.
// Safe to call concurrently with Run.
func (p *ONNXPipeline) SetLabels(labels []string) {
	cp := make([]string, len(labels))
	copy(cp, labels)
	p.labelsMu.Lock()
	p.labels = cp
	p.labelsMu.Unlock()
}

// load acquires the shared ONNX session for modelPath, loading it from disk
// if no other pipeline has done so yet.
func (p *ONNXPipeline) load(modelPath string) error {
	sess, err := acquireSession(modelPath)
	if err != nil {
		return err
	}
	p.shared = sess
	return nil
}

// runRaw tokenizes text, runs inference on GPU (falling back to CPU), and returns
// a JSON result. Concurrent calls on pipelines sharing the same model file are
// serialized through the shared session mutex.
func (p *ONNXPipeline) runRaw(text []byte) (json.RawMessage, error) {
	if p.shared == nil {
		return nil, fmt.Errorf("inference: onnx: model not loaded")
	}
	s := p.shared
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isZeroShot {
		return p.runZeroShot(s, text)
	}

	enc := s.tokenizer.EncodeWithOptions(
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

	s.fillInputs(ids, mask)
	if err := s.session.Run(); err != nil {
		return nil, fmt.Errorf("inference: onnx: run: %w", err)
	}

	logits := s.logitsTensor.GetData()
	if s.isNER {
		return postProcessNER(logits, s.id2label, s.numLabels, seqLen, tokens)
	}
	return postProcessClassification(logits, s.id2label)
}

// Run executes the ONNX model on input.Text and returns structured outputs.
func (p *ONNXPipeline) Run(_ context.Context, input PipelineInput) (map[string]any, error) {
	if labels, ok := input.Config["labels"].([]string); ok && len(labels) > 0 {
		p.SetLabels(labels)
	}
	raw, err := p.runRaw([]byte(input.Text))
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{"raw": string(raw)}, nil
	}
	return out, nil
}

// runZeroShot performs zero-shot classification using NLI: for each candidate
// label it encodes (text, hypothesis) as a pair, runs inference, and collects
// the entailment score. Called with s.mu already held.
func (p *ONNXPipeline) runZeroShot(s *sharedONNXSession, text []byte) (json.RawMessage, error) {
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
		input := string(text) + " [SEP] " + hypothesis

		enc := s.tokenizer.EncodeWithOptions(
			input, true,
			tokenizers.WithReturnAttentionMask(),
		)
		ids := enc.IDs
		mask := enc.AttentionMask
		if len(ids) > maxSeqLen {
			ids = ids[:maxSeqLen]
			mask = mask[:maxSeqLen]
		}

		s.fillInputs(ids, mask)
		if err := s.session.Run(); err != nil {
			return nil, fmt.Errorf("inference: onnx: zero-shot [%s]: %w", label, err)
		}

		logits := s.logitsTensor.GetData()
		probs := softmax(logits)
		entailmentLogits[i] = probs[s.entailmentIdx]
	}

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

// Close releases this pipeline's reference to the shared session. The session
// is destroyed only when all pipelines referencing it have been closed.
func (p *ONNXPipeline) Close() error {
	if p.shared == nil {
		return nil
	}
	err := releaseSession(p.shared)
	p.shared = nil
	return err
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
// graph input. Reads only the first 16 KB to avoid loading the full model.
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
