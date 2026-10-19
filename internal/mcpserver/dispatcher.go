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
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

// Dispatcher translates MCP tool calls into HTTP requests against the tidewatch API.
// If username and password are provided it manages token acquisition and re-authentication
// transparently; otherwise it uses a static token (or no auth).
type Dispatcher struct {
	baseURL     string
	username    string
	password    string
	mu          sync.Mutex // guards cachedToken
	cachedToken string
	client      *http.Client
}

// NewDispatcher creates a Dispatcher with no authentication.
func NewDispatcher(baseURL string) *Dispatcher {
	return &Dispatcher{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// NewDispatcherWithCredentials creates a Dispatcher that logs in with username/password,
// caches the resulting JWT, and re-authenticates automatically on 401.
func NewDispatcherWithCredentials(baseURL, username, password string) *Dispatcher {
	return &Dispatcher{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// token returns the current cached token, acquiring one first if needed.
func (d *Dispatcher) token(ctx context.Context) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cachedToken != "" {
		return d.cachedToken, nil
	}
	return d.login(ctx)
}

// invalidate clears the cached token so the next call triggers a fresh login.
func (d *Dispatcher) invalidate() {
	d.mu.Lock()
	d.cachedToken = ""
	d.mu.Unlock()
}

// login calls POST /api/auth/login and caches the returned JWT. Caller must hold d.mu.
func (d *Dispatcher) login(ctx context.Context) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"username": d.username,
		"password": d.password,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/api/auth/login", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("login request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login failed (%d): %s", resp.StatusCode, respBody)
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil || result.Token == "" {
		return "", fmt.Errorf("invalid login response: %s", respBody)
	}
	d.cachedToken = result.Token
	return d.cachedToken, nil
}

// Handle executes the REST call described by td with arguments from req.
func (d *Dispatcher) Handle(ctx context.Context, td ToolDef, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	result, retry, err := d.do(ctx, td, req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if retry {
		// Token was invalid — credentials have been refreshed, try once more.
		result, _, err = d.do(ctx, td, req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
	}
	return result, nil
}

// Execute looks up the ToolDef by name, builds a CallToolRequest from args,
// and calls Handle. Satisfies inference.ToolExecutor.
func (d *Dispatcher) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
	td, ok := toolIndex[name]
	if !ok {
		return "", fmt.Errorf("dispatcher: unknown tool: %s", name)
	}
	var req mcp.CallToolRequest
	req.Params.Arguments = args
	result, err := d.Handle(ctx, td, req)
	if err != nil {
		return "", err
	}
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text, nil
		}
	}
	return "", nil
}

// do performs the HTTP request. Returns (result, needsRetry, error).
// needsRetry is true when a 401 was received and credentials were refreshed.
func (d *Dispatcher) do(ctx context.Context, td ToolDef, req mcp.CallToolRequest) (*mcp.CallToolResult, bool, error) {
	args := req.GetArguments()

	// Build path — substitute {param} tokens.
	path := td.Path
	for _, p := range td.PathParams {
		val, ok := args[p.Name]
		if !ok {
			return mcp.NewToolResultError(fmt.Sprintf("missing required path param: %s", p.Name)), false, nil
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
	var bodyBytes []byte
	if td.Body != nil {
		if bodyVal, ok := args["body"]; ok {
			var err error
			bodyBytes, err = json.Marshal(bodyVal)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to marshal body: %v", err)), false, nil
			}
		}
	}

	var bodyReader io.Reader
	if len(bodyBytes) > 0 {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	httpReq, err := http.NewRequestWithContext(ctx, td.Method, rawURL, bodyReader)
	if err != nil {
		return nil, false, fmt.Errorf("failed to build request: %w", err)
	}
	if len(bodyBytes) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	// Attach auth header.
	if d.username != "" {
		tok, err := d.token(ctx)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("authentication failed: %v", err)), false, nil
		}
		httpReq.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := d.client.Do(httpReq)
	if err != nil {
		return nil, false, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read response: %w", err)
	}

	// On 401 with credential-based auth, invalidate token and signal a retry.
	if resp.StatusCode == http.StatusUnauthorized && d.username != "" {
		d.invalidate()
		return nil, true, nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return mcp.NewToolResultError(string(respBody)), false, nil
	}
	if len(respBody) == 0 {
		return mcp.NewToolResultText(fmt.Sprintf("OK (%d)", resp.StatusCode)), false, nil
	}
	return mcp.NewToolResultText(string(respBody)), false, nil
}
