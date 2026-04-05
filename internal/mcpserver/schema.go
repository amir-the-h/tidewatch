package mcpserver

import "github.com/mark3labs/mcp-go/mcp"

// BuildToolOptions converts a ToolDef into a slice of mcp.ToolOption
// ready to pass to mcp.NewTool.
func BuildToolOptions(td ToolDef) []mcp.ToolOption {
	opts := []mcp.ToolOption{mcp.WithDescription(td.Description)}

	for _, p := range td.PathParams {
		opts = append(opts, buildParamOption(p))
	}
	for _, p := range td.QueryParams {
		opts = append(opts, buildParamOption(p))
	}
	if td.Body != nil {
		var propOpts []mcp.PropertyOption
		if td.Body.Description != "" {
			propOpts = append(propOpts, mcp.Description(td.Body.Description))
		}
		opts = append(opts, mcp.WithObject("body", propOpts...))
	}
	return opts
}

func buildParamOption(p ParamDef) mcp.ToolOption {
	var propOpts []mcp.PropertyOption
	if p.Description != "" {
		propOpts = append(propOpts, mcp.Description(p.Description))
	}
	if p.Required {
		propOpts = append(propOpts, mcp.Required())
	}
	switch p.Type {
	case "integer":
		return mcp.WithNumber(p.Name, propOpts...)
	case "boolean":
		return mcp.WithBoolean(p.Name, propOpts...)
	default: // "string"
		return mcp.WithString(p.Name, propOpts...)
	}
}
