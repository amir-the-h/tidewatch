package transform

import "fmt"

// GroupEntities reads an array of entity objects from the pipeline output,
// filters by a minimum score, and groups the word values by label into string
// slices. The source field is removed and each label becomes a top-level key.
//
// Config fields (all optional):
//
//	source      — field holding the entities array       (default: "entities")
//	min_score   — score threshold, exclusive             (default: 0.8)
//	label_field — entity sub-field used as the map key   (default: "label")
//	word_field  — entity sub-field appended to each list (default: "word")
//	score_field — entity sub-field compared to min_score (default: "score")
//
// Example: given entities [{label:"LOC", word:"Iran", score:0.999}, {label:"LOC", word:"Tehran", score:0.75}]
// with default min_score 0.8, the result is {"LOC": ["Iran"]} (Tehran is filtered out).
type GroupEntities struct {
	source     string
	minScore   float64
	labelField string
	wordField  string
	scoreField string
}

func newGroupEntities(spec map[string]interface{}) (*GroupEntities, error) {
	source, _ := spec["source"].(string)
	if source == "" {
		source = "entities"
	}
	labelField, _ := spec["label_field"].(string)
	if labelField == "" {
		labelField = "label"
	}
	wordField, _ := spec["word_field"].(string)
	if wordField == "" {
		wordField = "word"
	}
	scoreField, _ := spec["score_field"].(string)
	if scoreField == "" {
		scoreField = "score"
	}
	minScore := 0.8
	if v, ok := spec["min_score"].(float64); ok {
		minScore = v
	}
	return &GroupEntities{
		source:     source,
		minScore:   minScore,
		labelField: labelField,
		wordField:  wordField,
		scoreField: scoreField,
	}, nil
}

func (g *GroupEntities) Apply(output map[string]any) (map[string]any, error) {
	raw, ok := output[g.source]
	if !ok {
		return output, nil
	}

	items, ok := raw.([]interface{})
	if !ok {
		return output, fmt.Errorf("group_entities: %q must be an array, got %T", g.source, raw)
	}

	grouped := map[string][]string{}
	for i, item := range items {
		obj, ok := item.(map[string]interface{})
		if !ok {
			return output, fmt.Errorf("group_entities: %s[%d] must be an object, got %T", g.source, i, item)
		}

		score, err := toFloat64Field(obj, g.scoreField)
		if err != nil {
			return output, fmt.Errorf("group_entities: %s[%d].%s: %w", g.source, i, g.scoreField, err)
		}
		if score <= g.minScore {
			continue
		}

		label, ok := obj[g.labelField].(string)
		if !ok {
			return output, fmt.Errorf("group_entities: %s[%d].%s must be a string", g.source, i, g.labelField)
		}
		word, ok := obj[g.wordField].(string)
		if !ok {
			return output, fmt.Errorf("group_entities: %s[%d].%s must be a string", g.source, i, g.wordField)
		}

		grouped[label] = append(grouped[label], word)
	}

	result := make(map[string]any, len(output)-1+len(grouped))
	for k, v := range output {
		if k == g.source {
			continue
		}
		result[k] = v
	}
	for label, words := range grouped {
		result[label] = words
	}
	return result, nil
}

func toFloat64Field(obj map[string]interface{}, field string) (float64, error) {
	v, ok := obj[field]
	if !ok {
		return 0, fmt.Errorf("field %q not found", field)
	}
	switch f := v.(type) {
	case float64:
		return f, nil
	case float32:
		return float64(f), nil
	default:
		return 0, fmt.Errorf("expected number, got %T", v)
	}
}
