package mcpserver

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Server wraps the mcp-go server and dispatcher.
type Server struct {
	mcp        *server.MCPServer
	dispatcher *Dispatcher
}

// New creates a Server that proxies all iran-watcher endpoints to baseURL with no auth.
func New(baseURL string) *Server {
	return NewWithCredentials(baseURL, "", "")
}

// NewWithCredentials creates a Server that logs in with username/password and manages
// token acquisition and rotation automatically.
func NewWithCredentials(baseURL, username, password string) *Server {
	d := NewDispatcherWithCredentials(baseURL, username, password)

	s := server.NewMCPServer("iran-watcher", "1.0.0")

	for _, td := range AllTools() {
		td := td // capture loop variable for closure
		tool := mcp.NewTool(td.Name, BuildToolOptions(td)...)
		s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return d.Handle(ctx, td, req)
		})
	}

	return &Server{mcp: s, dispatcher: d}
}

// ServeStdio serves the MCP protocol over stdin/stdout. Blocks until the
// client disconnects or an error occurs.
func (s *Server) ServeStdio(_ context.Context) error {
	return server.ServeStdio(s.mcp)
}

// ServeHTTP starts an SSE server on addr (e.g. ":3001"). Blocks until stopped.
func (s *Server) ServeHTTP(addr string) error {
	sseServer := server.NewSSEServer(s.mcp,
		server.WithBaseURL(fmt.Sprintf("http://%s", addr)),
	)
	return sseServer.Start(addr)
}
