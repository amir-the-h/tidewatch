package transform

import "fmt"

// Transformer post-processes a pipeline's output map before the result is
// persisted to action_execution_metadata and passed to downstream chains.
// Implementations must not modify the input map in place; return a new map.
type Transformer interface {
	Apply(output map[string]any) (map[string]any, error)
}

// Parse builds a []Transformer from the "transformers" key in a pipeline config.
// Unrecognised transformer types and invalid specs are returned as errors.
// Returns nil, nil when no "transformers" key is present.
func Parse(config map[string]any) ([]Transformer, error) {
	raw, ok := config["transformers"]
	if !ok {
		return nil, nil
	}
	specs, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("transform: 'transformers' must be an array")
	}
	out := make([]Transformer, 0, len(specs))
	for i, s := range specs {
		spec, ok := s.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("transform: transformer[%d] must be an object", i)
		}
		t, err := build(spec)
		if err != nil {
			return nil, fmt.Errorf("transform: transformer[%d]: %w", i, err)
		}
		out = append(out, t)
	}
	return out, nil
}

func build(spec map[string]interface{}) (Transformer, error) {
	typ, _ := spec["type"].(string)
	switch typ {
	case "zip_arrays":
		return newZipArrays(spec)
	case "normalize":
		return newNormalize(spec)
	case "multiply":
		return newMultiply(spec)
	case "group_entities":
		return newGroupEntities(spec)
	default:
		return nil, fmt.Errorf("unknown transformer type %q", typ)
	}
}
