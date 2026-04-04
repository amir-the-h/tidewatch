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

// New creates a Server that proxies all iran-watcher endpoints to baseURL.
func New(baseURL string) *Server {
	d := NewDispatcher(baseURL)

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
