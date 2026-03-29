package transform

import (
	"fmt"
	"log/slog"
	"math"
)

// Normalize scales a set of numeric fields in the output.
//
// Config fields (all optional):
//
//	fields    — keys to normalize; if absent, all top-level float64/float32 values
//	method    — "sum" (default), "minmax", or "softmax"
//	range_min — output minimum for minmax (default 0)
//	range_max — output maximum for minmax (default 1)
//
// "sum"     — divides each value by the total so they sum to 1.
// "minmax"  — scales values to [range_min, range_max].
// "softmax" — applies the softmax function so values sum to 1 and are non-negative.
type Normalize struct {
	fields   []string // nil = all numeric fields
	method   string
	rangeMin float64
	rangeMax float64
}

func newNormalize(spec map[string]interface{}) (*Normalize, error) {
	method, _ := spec["method"].(string)
	if method == "" {
		method = "sum"
	}
	if method != "sum" && method != "minmax" && method != "softmax" {
		return nil, fmt.Errorf("normalize: unknown method %q; want sum, minmax, or softmax", method)
	}

	var fields []string
	if raw, ok := spec["fields"]; ok {
		fs, err := toStringSlice(raw)
		if err != nil {
			return nil, fmt.Errorf("normalize: fields: %w", err)
		}
		fields = fs
	}

	rangeMin := 0.0
	rangeMax := 1.0
	if v, ok := spec["range_min"].(float64); ok {
		rangeMin = v
	}
	if v, ok := spec["range_max"].(float64); ok {
		rangeMax = v
	}
	if rangeMin >= rangeMax {
		return nil, fmt.Errorf("normalize: range_min (%v) must be less than range_max (%v)", rangeMin, rangeMax)
	}

	return &Normalize{fields: fields, method: method, rangeMin: rangeMin, rangeMax: rangeMax}, nil
}

func (n *Normalize) Apply(output map[string]any) (map[string]any, error) {
	keys, vals := n.collect(output)
	if len(keys) == 0 {
		return output, nil
	}

	slog.Debug("transform: normalize: invoked", "method", n.method, "fields", keys)

	if n.method == "minmax" {
		before := make(map[string]float64, len(keys))
		for i, k := range keys {
			before[k] = vals[i]
		}
		slog.Debug("transform: normalize minmax: before",
			"range", fmt.Sprintf("[%v, %v]", n.rangeMin, n.rangeMax),
			"fields", keys,
			"values", before,
		)
	}

	var normalized []float64
	var err error
	switch n.method {
	case "sum":
		normalized, err = normSum(vals)
	case "minmax":
		normalized = normMinMax(vals, n.rangeMin, n.rangeMax)
	case "softmax":
		normalized = normSoftmax(vals)
	}
	if err != nil {
		return output, fmt.Errorf("normalize (%s): %w", n.method, err)
	}

	if n.method == "minmax" {
		after := make(map[string]float64, len(keys))
		for i, k := range keys {
			after[k] = normalized[i]
		}
		slog.Debug("transform: normalize minmax: after",
			"range", fmt.Sprintf("[%v, %v]", n.rangeMin, n.rangeMax),
			"fields", keys,
			"values", after,
		)
	}

	result := make(map[string]any, len(output))
	for k, v := range output {
		result[k] = v
	}
	for i, k := range keys {
		result[k] = normalized[i]
	}
	return result, nil
}

// collect returns the keys and float64 values to normalize.
func (n *Normalize) collect(output map[string]any) ([]string, []float64) {
	var keys []string
	var vals []float64

	pick := func(k string, v any) {
		switch f := v.(type) {
		case float64:
			keys = append(keys, k)
			vals = append(vals, f)
		case float32:
			keys = append(keys, k)
			vals = append(vals, float64(f))
		}
	}

	if len(n.fields) > 0 {
		for _, k := range n.fields {
			v, ok := output[k]
			if !ok {
				continue
			}
			pick(k, v)
		}
	} else {
		for k, v := range output {
			pick(k, v)
		}
	}
	return keys, vals
}

// Multiply scales each selected numeric value by a constant factor.
//
// Config fields:
//
//	factor — the multiplier (required, must be non-zero)
//	fields — keys to multiply; if absent, all top-level float64/float32 values
type Multiply struct {
	factor float64
	fields []string
}

func newMultiply(spec map[string]interface{}) (*Multiply, error) {
	factor, ok := spec["factor"].(float64)
	if !ok {
		return nil, fmt.Errorf("multiply: 'factor' is required and must be a number")
	}
	if factor == 0 {
		return nil, fmt.Errorf("multiply: 'factor' must be non-zero")
	}
	var fields []string
	if raw, ok := spec["fields"]; ok {
		fs, err := toStringSlice(raw)
		if err != nil {
			return nil, fmt.Errorf("multiply: fields: %w", err)
		}
		fields = fs
	}
	return &Multiply{factor: factor, fields: fields}, nil
}

func (m *Multiply) Apply(output map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(output))
	for k, v := range output {
		result[k] = v
	}

	apply := func(k string, v any) {
		switch f := v.(type) {
		case float64:
			result[k] = f * m.factor
		case float32:
			result[k] = float64(f) * m.factor
		}
	}

	if len(m.fields) > 0 {
		for _, k := range m.fields {
			if v, ok := output[k]; ok {
				apply(k, v)
			}
		}
	} else {
		for k, v := range output {
			apply(k, v)
		}
	}
	return result, nil
}

func normSum(vals []float64) ([]float64, error) {
	var sum float64
	for _, v := range vals {
		sum += v
	}
	if sum == 0 {
		return nil, fmt.Errorf("sum of values is zero, cannot normalize")
	}
	out := make([]float64, len(vals))
	for i, v := range vals {
		out[i] = v / sum
	}
	return out, nil
}

func normMinMax(vals []float64, outMin, outMax float64) []float64 {
	min, max := vals[0], vals[0]
	for _, v := range vals[1:] {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	out := make([]float64, len(vals))
	span := max - min
	outSpan := outMax - outMin
	if span == 0 {
		// All values equal; map to midpoint of output range.
		mid := outMin + outSpan/2
		for i := range out {
			out[i] = mid
		}
		return out
	}
	for i, v := range vals {
		out[i] = outMin + (v-min)/span*outSpan
	}
	return out
}

func normSoftmax(vals []float64) []float64 {
	max := vals[0]
	for _, v := range vals[1:] {
		if v > max {
			max = v
		}
	}
	out := make([]float64, len(vals))
	var sum float64
	for i, v := range vals {
		e := math.Exp(v - max)
		out[i] = e
		sum += e
	}
	for i := range out {
		out[i] /= sum
	}
	return out
}
