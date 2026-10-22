package source

import (
	"encoding/json"
	"fmt"
)

// Mask replaces a secret value in API responses. A write that sends Mask for a
// secret key keeps the stored value.
const Mask = "••••"

// SecretKeys returns the keys of the secret fields.
func SecretKeys(fields []Field) []string {
	var keys []string
	for _, f := range fields {
		if f.Secret {
			keys = append(keys, f.Key)
		}
	}
	return keys
}

// MaskConfig returns cfg with every non-empty secret key replaced by Mask. A
// config that isn't a JSON object is returned unchanged.
func MaskConfig(secretKeys []string, cfg json.RawMessage) json.RawMessage {
	if len(secretKeys) == 0 || len(cfg) == 0 {
		return cfg
	}
	var m map[string]any
	if err := json.Unmarshal(cfg, &m); err != nil {
		return cfg
	}
	changed := false
	for _, k := range secretKeys {
		if v, ok := m[k]; ok && v != nil && v != "" {
			m[k] = Mask
			changed = true
		}
	}
	if !changed {
		return cfg
	}
	out, err := json.Marshal(m)
	if err != nil {
		return cfg
	}
	return out
}

// MergeSecrets returns incoming with every secret key whose value is Mask
// replaced by the stored value (or removed when nothing is stored), so a
// client can send back the masked config it read without losing secrets.
func MergeSecrets(secretKeys []string, incoming, stored json.RawMessage) (json.RawMessage, error) {
	if len(secretKeys) == 0 || len(incoming) == 0 {
		return incoming, nil
	}
	var in map[string]any
	if err := json.Unmarshal(incoming, &in); err != nil {
		return nil, fmt.Errorf("config must be a JSON object: %w", err)
	}
	var old map[string]any
	if len(stored) > 0 {
		_ = json.Unmarshal(stored, &old)
	}
	changed := false
	for _, k := range secretKeys {
		if in[k] != Mask {
			continue
		}
		changed = true
		if v, ok := old[k]; ok {
			in[k] = v
		} else {
			delete(in, k)
		}
	}
	if !changed {
		return incoming, nil
	}
	return json.Marshal(in)
}
