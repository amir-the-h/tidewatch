package inference

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"

	tokenizers "github.com/daulet/tokenizers"
	ort "github.com/yalue/onnxruntime_go"
)

const defaultMaxNewTokens = 64

// seq2seqConfig holds the fields we read from config.json for seq2seq models.
type seq2seqConfig struct {
	DModel    int `json:"d_model"`    // BART / mBART hidden size
	HiddenSize int `json:"hidden_size"` // T5 and other architectures
	VocabSize int `json:"vocab_size"`
}

// genConfig holds optional values from generation_config.json.
type genConfig struct {
	MaxNewTokens        int   `json:"max_new_tokens"`
	MaxLength           int   `json:"max_length"`
	MinLength           int   `json:"min_length"`
	EosTokenID          int64 `json:"eos_token_id"`
	DecoderStartTokenID int64 `json:"decoder_start_token_id"`
	PadTokenID          int64 `json:"pad_token_id"`
	NoRepeatNgramSize   int   `json:"no_repeat_ngram_size"`
}

// Seq2SeqPipeline implements SummarizationPipeline using a pair of ONNX
// encoder/decoder models (Optimum export format). modelPath must point to
// encoder_model.onnx; the decoder is inferred from the same directory.
type Seq2SeqPipeline struct {
	name string

	tokenizer   *tokenizers.Tokenizer
	encoderSess *ort.DynamicAdvancedSession
	decoderSess *ort.DynamicAdvancedSession

	hiddenSize        int
	vocabSize         int
	maxNewTokens      int
	minNewTokens      int
	eosTokenID        int64
	decoderStart      int64 // decoder_start_token_id
	noRepeatNgramSize int

	mu sync.Mutex
}

// NewSeq2SeqPipeline creates a new Seq2SeqPipeline with the given model name.
func NewSeq2SeqPipeline(name string) *Seq2SeqPipeline {
	return &Seq2SeqPipeline{name: name, maxNewTokens: defaultMaxNewTokens}
}

// NewSeq2SeqPipelineFromConfig creates a Seq2Seq pipeline from DB pipeline config.
func NewSeq2SeqPipelineFromConfig(name string, config map[string]any) (*Seq2SeqPipeline, error) {
	path, _ := config["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("inference: seq2seq: config missing 'path'")
	}
	p := NewSeq2SeqPipeline(name)
	if err := p.load(path); err != nil {
		return nil, err
	}
	return p, nil
}

// Name returns the model name as registered in the models table.
func (p *Seq2SeqPipeline) Name() string { return p.name }

// Type returns the pipeline type.
func (p *Seq2SeqPipeline) Type() PipelineType { return PipelineTypeONNX }

// load initialises the encoder and decoder ONNX sessions.
// modelPath should point to encoder_model.onnx; decoder_model.onnx must be
// in the same directory.  The tokenizer.json / config.json may sit in the
// same directory or one level up (e.g. when ONNX files live in an onnx/ subdir).
func (p *Seq2SeqPipeline) load(modelPath string) error {
	dir := filepath.Dir(modelPath)
	base := filepath.Base(modelPath)

	encoderPath := modelPath
	// Derive decoder path from encoder path.
	decoderBase := strings.Replace(base, "encoder_model", "decoder_model", 1)
	if decoderBase == base {
		decoderBase = "decoder_model.onnx"
	}
	decoderPath := filepath.Join(dir, decoderBase)

	// Tokenizer may be in the model dir or its parent (Optimum onnx/ subdir layout).
	tokenizerDir := dir
	if _, err := os.Stat(filepath.Join(dir, "tokenizer.json")); os.IsNotExist(err) {
		tokenizerDir = filepath.Dir(dir)
	}

	tok, err := tokenizers.FromFile(filepath.Join(tokenizerDir, "tokenizer.json"))
	if err != nil {
		return fmt.Errorf("inference: seq2seq: load tokenizer: %w", err)
	}
	p.tokenizer = tok

	// Read architecture info from config.json.
	s2cfg, err := loadSeq2SeqConfig(tokenizerDir)
	if err != nil {
		return fmt.Errorf("inference: seq2seq: load config: %w", err)
	}
	p.hiddenSize = s2cfg.HiddenSize
	if p.hiddenSize == 0 {
		p.hiddenSize = s2cfg.DModel
	}
	if p.hiddenSize == 0 {
		return fmt.Errorf("inference: seq2seq: cannot determine hidden size from config.json")
	}
	p.vocabSize = s2cfg.VocabSize
	if p.vocabSize == 0 {
		p.vocabSize = int(p.tokenizer.VocabSize())
	}

	// Read generation parameters (eos / decoder-start token IDs).
	gcfg := loadGenConfig(tokenizerDir)
	p.eosTokenID = gcfg.EosTokenID
	p.decoderStart = gcfg.DecoderStartTokenID
	if p.decoderStart == 0 {
		p.decoderStart = p.eosTokenID
	}
	if gcfg.MaxNewTokens > 0 {
		p.maxNewTokens = gcfg.MaxNewTokens
	} else if gcfg.MaxLength > 0 {
		p.maxNewTokens = gcfg.MaxLength
	}
	p.minNewTokens = gcfg.MinLength
	p.noRepeatNgramSize = gcfg.NoRepeatNgramSize

	// Create encoder session.
	encOpts, err := ort.NewSessionOptions()
	if err != nil {
		return fmt.Errorf("inference: seq2seq: encoder session options: %w", err)
	}
	defer encOpts.Destroy()

	p.encoderSess, err = ort.NewDynamicAdvancedSession(
		encoderPath,
		[]string{"input_ids", "attention_mask"},
		[]string{"last_hidden_state"},
		encOpts,
	)
	if err != nil {
		return fmt.Errorf("inference: seq2seq: create encoder session: %w", err)
	}

	// Create decoder session.
	decOpts, err := ort.NewSessionOptions()
	if err != nil {
		return fmt.Errorf("inference: seq2seq: decoder session options: %w", err)
	}
	defer decOpts.Destroy()

	p.decoderSess, err = ort.NewDynamicAdvancedSession(
		decoderPath,
		[]string{"input_ids", "encoder_attention_mask", "encoder_hidden_states"},
		[]string{"logits"},
		decOpts,
	)
	if err != nil {
		return fmt.Errorf("inference: seq2seq: create decoder session: %w", err)
	}

	return nil
}

// Run executes the pipeline on input.Text and returns {"summary": "..."}.
func (p *Seq2SeqPipeline) Run(ctx context.Context, input PipelineInput) (map[string]any, error) {
	s, err := p.Summarize(ctx, input.Text)
	if err != nil {
		return nil, err
	}
	return map[string]any{"summary": s}, nil
}

// Summarize generates a text summary using greedy decoding.
func (p *Seq2SeqPipeline) Summarize(_ context.Context, text string) (string, error) {
	if p.encoderSess == nil || p.decoderSess == nil {
		return "", fmt.Errorf("inference: seq2seq: model not loaded")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Tokenize input.
	enc := p.tokenizer.EncodeWithOptions(
		text, true,
		tokenizers.WithReturnAttentionMask(),
	)
	ids := enc.IDs
	mask := enc.AttentionMask
	if len(ids) > maxSeqLen {
		ids = ids[:maxSeqLen]
		mask = mask[:maxSeqLen]
	}
	seqLen := len(ids)

	// Build encoder input tensors.
	inputIDsData := make([]int64, seqLen)
	attnMaskData := make([]int64, seqLen)
	for i, id := range ids {
		inputIDsData[i] = int64(id)
		attnMaskData[i] = int64(mask[i])
	}

	encShape := ort.NewShape(1, int64(seqLen))

	encInputIDs, err := ort.NewTensor(encShape, inputIDsData)
	if err != nil {
		return "", fmt.Errorf("inference: seq2seq: encoder input_ids: %w", err)
	}
	defer encInputIDs.Destroy()

	encAttnMask, err := ort.NewTensor(encShape, attnMaskData)
	if err != nil {
		return "", fmt.Errorf("inference: seq2seq: encoder attention_mask: %w", err)
	}
	defer encAttnMask.Destroy()

	// Run encoder.
	encOutput, err := ort.NewEmptyTensor[float32](ort.NewShape(1, int64(seqLen), int64(p.hiddenSize)))
	if err != nil {
		return "", fmt.Errorf("inference: seq2seq: encoder output tensor: %w", err)
	}
	defer encOutput.Destroy()

	if err := p.encoderSess.Run(
		[]ort.Value{encInputIDs, encAttnMask},
		[]ort.Value{encOutput},
	); err != nil {
		return "", fmt.Errorf("inference: seq2seq: encoder run: %w", err)
	}

	// Copy hidden state so we can safely reuse it across decode steps.
	src := encOutput.GetData()
	hiddenState := make([]float32, len(src))
	copy(hiddenState, src)

	hiddenShape := ort.NewShape(1, int64(seqLen), int64(p.hiddenSize))

	// Greedy decode: start with decoder_start_token_id.
	decoderIDs := []int64{p.decoderStart}

	for step := 0; step < p.maxNewTokens; step++ {
		decLen := len(decoderIDs)
		decShape := ort.NewShape(1, int64(decLen))

		// Decoder input IDs — copy slice so the tensor has stable backing memory.
		decIDsCopy := make([]int64, decLen)
		copy(decIDsCopy, decoderIDs)

		decInputIDs, err := ort.NewTensor(decShape, decIDsCopy)
		if err != nil {
			return "", fmt.Errorf("inference: seq2seq: decoder input_ids step %d: %w", step, err)
		}

		// Encoder attention mask for the decoder.
		encMaskCopy := make([]int64, seqLen)
		copy(encMaskCopy, attnMaskData)
		decEncMask, err := ort.NewTensor(encShape, encMaskCopy)
		if err != nil {
			decInputIDs.Destroy()
			return "", fmt.Errorf("inference: seq2seq: decoder enc_mask step %d: %w", step, err)
		}

		// Encoder hidden states for the decoder.
		hiddenCopy := make([]float32, len(hiddenState))
		copy(hiddenCopy, hiddenState)
		decEncHidden, err := ort.NewTensor(hiddenShape, hiddenCopy)
		if err != nil {
			decInputIDs.Destroy()
			decEncMask.Destroy()
			return "", fmt.Errorf("inference: seq2seq: decoder hidden_states step %d: %w", step, err)
		}

		// Output logits tensor: [1, decLen, vocab_size].
		logitsTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(1, int64(decLen), int64(p.vocabSize)))
		if err != nil {
			decInputIDs.Destroy()
			decEncMask.Destroy()
			decEncHidden.Destroy()
			return "", fmt.Errorf("inference: seq2seq: logits tensor step %d: %w", step, err)
		}

		runErr := p.decoderSess.Run(
			[]ort.Value{decInputIDs, decEncMask, decEncHidden},
			[]ort.Value{logitsTensor},
		)
		decInputIDs.Destroy()
		decEncMask.Destroy()
		decEncHidden.Destroy()

		if runErr != nil {
			logitsTensor.Destroy()
			return "", fmt.Errorf("inference: seq2seq: decoder run step %d: %w", step, runErr)
		}

		rawLogits := logitsTensor.GetData()
		offset := (decLen - 1) * p.vocabSize
		stepLogits := make([]float32, p.vocabSize)
		copy(stepLogits, rawLogits[offset:offset+p.vocabSize])
		logitsTensor.Destroy()

		generated := decoderIDs[1:]

		// Suppress EOS before min_length tokens have been generated.
		if p.minNewTokens > 0 && len(generated) < p.minNewTokens {
			stepLogits[p.eosTokenID] = float32(math.Inf(-1))
		}

		// No-repeat n-gram blocking.
		if p.noRepeatNgramSize > 1 && len(generated) >= p.noRepeatNgramSize-1 {
			prefix := generated[len(generated)-(p.noRepeatNgramSize-1):]
			for i := 0; i <= len(generated)-p.noRepeatNgramSize; i++ {
				match := true
				for j := 0; j < p.noRepeatNgramSize-1; j++ {
					if generated[i+j] != prefix[j] {
						match = false
						break
					}
				}
				if match {
					blocked := generated[i+p.noRepeatNgramSize-1]
					if int(blocked) < len(stepLogits) {
						stepLogits[blocked] = float32(math.Inf(-1))
					}
				}
			}
		}

		nextID := int64(argmaxSeq(stepLogits))
		if nextID == p.eosTokenID {
			break
		}
		decoderIDs = append(decoderIDs, nextID)
	}

	// Decode output token IDs, skipping the leading decoder_start token.
	outputIDs := decoderIDs[1:]
	outputUint32 := make([]uint32, len(outputIDs))
	for i, id := range outputIDs {
		outputUint32[i] = uint32(id)
	}
	return p.tokenizer.Decode(outputUint32, true), nil
}

// Close releases ONNX sessions and the tokenizer.
func (p *Seq2SeqPipeline) Close() error {
	if p.encoderSess != nil {
		if err := p.encoderSess.Destroy(); err != nil {
			return fmt.Errorf("inference: seq2seq: destroy encoder: %w", err)
		}
		p.encoderSess = nil
	}
	if p.decoderSess != nil {
		if err := p.decoderSess.Destroy(); err != nil {
			return fmt.Errorf("inference: seq2seq: destroy decoder: %w", err)
		}
		p.decoderSess = nil
	}
	if p.tokenizer != nil {
		_ = p.tokenizer.Close()
		p.tokenizer = nil
	}
	return nil
}

// argmaxSeq returns the index of the maximum value in v.
func argmaxSeq(v []float32) int {
	best := 0
	for i := 1; i < len(v); i++ {
		if v[i] > v[best] {
			best = i
		}
	}
	return best
}

// ── config helpers ────────────────────────────────────────────────────────────

func loadSeq2SeqConfig(dir string) (*seq2seqConfig, error) {
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return nil, fmt.Errorf("read config.json: %w", err)
	}
	var cfg seq2seqConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config.json: %w", err)
	}
	return &cfg, nil
}

func loadGenConfig(dir string) genConfig {
	var cfg genConfig
	data, err := os.ReadFile(filepath.Join(dir, "generation_config.json"))
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	return cfg
}
