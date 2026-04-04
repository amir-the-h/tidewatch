package mcpserver

// ToolDef declares one REST endpoint as an MCP tool.
type ToolDef struct {
	Name        string
	Description string
	Method      string     // GET, POST, PUT, DELETE
	Path        string     // e.g. "/api/tweets/{id}"
	PathParams  []ParamDef // always required; substituted into path template
	QueryParams []ParamDef // optional query string fields
	Body        *BodyDef   // nil if endpoint takes no request body
}

// ParamDef describes a single parameter.
type ParamDef struct {
	Name        string
	Description string
	Required    bool
	Type        string // "string" | "integer" | "boolean"
}

// BodyDef describes a request body.
type BodyDef struct {
	Description string
}
