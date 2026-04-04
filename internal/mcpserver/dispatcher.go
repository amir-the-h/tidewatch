package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// Dispatcher translates MCP tool calls into HTTP requests against the iran-watcher API.
type Dispatcher struct {
	baseURL string
	client  *http.Client
}

// NewDispatcher creates a Dispatcher that forwards tool calls to baseURL.
func NewDispatcher(baseURL string) *Dispatcher {
	return &Dispatcher{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{},
	}
}

// Handle executes the REST call described by td with arguments from req.
func (d *Dispatcher) Handle(ctx context.Context, td ToolDef, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	// Build path — substitute {param} tokens.
	path := td.Path
	for _, p := range td.PathParams {
		val, ok := args[p.Name]
		if !ok {
			return mcp.NewToolResultError(fmt.Sprintf("missing required path param: %s", p.Name)), nil
		}
		path = strings.ReplaceAll(path, "{"+p.Name+"}", fmt.Sprintf("%v", val))
	}

	// Build query string.
	q := url.Values{}
	for _, p := range td.QueryParams {
		if val, ok := args[p.Name]; ok {
			q.Set(p.Name, fmt.Sprintf("%v", val))
		}
	}
	rawURL := d.baseURL + path
	if len(q) > 0 {
		rawURL += "?" + q.Encode()
	}

	// Build body.
	var bodyReader io.Reader
	if td.Body != nil {
		if bodyVal, ok := args["body"]; ok {
			b, err := json.Marshal(bodyVal)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to marshal body: %v", err)), nil
			}
			bodyReader = bytes.NewReader(b)
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, td.Method, rawURL, bodyReader)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to build request: %v", err)), nil
	}
	if bodyReader != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := d.client.Do(httpReq)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("request failed: %v", err)), nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to read response: %v", err)), nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mcp.NewToolResultError(string(respBody)), nil
	}
	if len(respBody) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("OK (%d)", resp.StatusCode)), nil
	}
	return mcp.NewToolResultText(string(respBody)), nil
}
