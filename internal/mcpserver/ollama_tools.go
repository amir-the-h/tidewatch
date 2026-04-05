package mcpserver

// OllamaTool describes one function tool in Ollama's JSON Schema format.
type OllamaTool struct {
	Type     string         `json:"type"` // always "function"
	Function OllamaToolFunc `json:"function"`
}

// OllamaToolFunc is the function definition within an OllamaTool.
type OllamaToolFunc struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"` // JSON Schema object
}

// BuildOllamaTools converts registered ToolDefs to Ollama's JSON Schema format.
// allowed is a list of tool names to include; nil or empty means all tools.
// Called at request time by the chat handler.
func BuildOllamaTools(allowed []string) []OllamaTool {
	all := AllTools()
	if len(allowed) == 0 {
		out := make([]OllamaTool, 0, len(all))
		for _, td := range all {
			out = append(out, toolDefToOllama(td))
		}
		return out
	}
	filter := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		filter[name] = true
	}
	out := make([]OllamaTool, 0, len(allowed))
	for _, td := range all {
		if filter[td.Name] {
			out = append(out, toolDefToOllama(td))
		}
	}
	return out
}

// toolIndex is a name→ToolDef lookup built once at package init.
var toolIndex map[string]ToolDef

func init() {
	all := AllTools()
	toolIndex = make(map[string]ToolDef, len(all))
	for _, td := range all {
		toolIndex[td.Name] = td
	}
}

func toolDefToOllama(td ToolDef) OllamaTool {
	props := map[string]interface{}{}
	var required []string

	for _, p := range td.PathParams {
		props[p.Name] = paramToSchemaObj(p)
		if p.Required {
			required = append(required, p.Name)
		}
	}
	for _, p := range td.QueryParams {
		props[p.Name] = paramToSchemaObj(p)
		if p.Required {
			required = append(required, p.Name)
		}
	}
	if td.Body != nil {
		obj := map[string]interface{}{"type": "object"}
		if td.Body.Description != "" {
			obj["description"] = td.Body.Description
		}
		props["body"] = obj
	}

	params := map[string]interface{}{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		params["required"] = required
	}

	return OllamaTool{
		Type: "function",
		Function: OllamaToolFunc{
			Name:        td.Name,
			Description: td.Description,
			Parameters:  params,
		},
	}
}

func paramToSchemaObj(p ParamDef) map[string]interface{} {
	obj := map[string]interface{}{}
	switch p.Type {
	case "integer":
		obj["type"] = "integer"
	case "boolean":
		obj["type"] = "boolean"
	default:
		obj["type"] = "string"
	}
	if p.Description != "" {
		obj["description"] = p.Description
	}
	return obj
}
