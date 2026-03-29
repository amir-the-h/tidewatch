package transform

import "fmt"

// ZipArrays zips two parallel arrays from the output into individual key-value
// pairs, removing the source arrays from the result.
//
// Config fields (all optional):
//
//	keys_field   — output key holding the labels array  (default: "labels")
//	values_field — output key holding the scores array  (default: "scores")
//
// Example: output {"labels":["a","b"], "scores":[0.9,0.1]} becomes {"a":0.9, "b":0.1}.
type ZipArrays struct {
	keysField   string
	valuesField string
}

func newZipArrays(spec map[string]interface{}) (*ZipArrays, error) {
	kf, _ := spec["keys_field"].(string)
	if kf == "" {
		kf = "labels"
	}
	vf, _ := spec["values_field"].(string)
	if vf == "" {
		vf = "scores"
	}
	return &ZipArrays{keysField: kf, valuesField: vf}, nil
}

func (z *ZipArrays) Apply(output map[string]any) (map[string]any, error) {
	keys, err := toStringSlice(output[z.keysField])
	if err != nil {
		return output, fmt.Errorf("zip_arrays: %s: %w", z.keysField, err)
	}
	vals, err := toFloat64Slice(output[z.valuesField])
	if err != nil {
		return output, fmt.Errorf("zip_arrays: %s: %w", z.valuesField, err)
	}
	if len(keys) != len(vals) {
		return output, fmt.Errorf("zip_arrays: %s (len %d) and %s (len %d) must have equal length",
			z.keysField, len(keys), z.valuesField, len(vals))
	}

	result := make(map[string]any, len(output)-2+len(keys))
	for k, v := range output {
		if k == z.keysField || k == z.valuesField {
			continue
		}
		result[k] = v
	}
	for i, k := range keys {
		result[k] = vals[i]
	}
	return result, nil
}

// toStringSlice converts a value that may be []string or []interface{} to []string.
func toStringSlice(v any) ([]string, error) {
	switch s := v.(type) {
	case []string:
		return s, nil
	case []interface{}:
		out := make([]string, len(s))
		for i, item := range s {
			str, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("element %d is not a string", i)
			}
			out[i] = str
		}
		return out, nil
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("expected array, got %T", v)
	}
}

// toFloat64Slice converts a value that may be []float64, []float32, or
// []interface{} (from JSON unmarshalling) to []float64.
func toFloat64Slice(v any) ([]float64, error) {
	switch s := v.(type) {
	case []float64:
		return s, nil
	case []float32:
		out := make([]float64, len(s))
		for i, f := range s {
			out[i] = float64(f)
		}
		return out, nil
	case []interface{}:
		out := make([]float64, len(s))
		for i, item := range s {
			switch f := item.(type) {
			case float64:
				out[i] = f
			case float32:
				out[i] = float64(f)
			default:
				return nil, fmt.Errorf("element %d is not a number", i)
			}
		}
		return out, nil
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("expected array, got %T", v)
	}
}
