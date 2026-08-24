package mcp

import (
	"io"
	"net/http"
)

// HTTPServer exposes the MCP server over HTTP (JSON-RPC POST). Any
// external MCP client (Claude Desktop, MCP inspector) can connect.
type HTTPServer struct {
	mcp *Server
}

func NewHTTPServer(mcp *Server) *HTTPServer {
	return &HTTPServer{mcp: mcp}
}

func (h *HTTPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	resp, err := h.mcp.Handle(r.Context(), body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp)
}
