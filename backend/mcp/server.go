package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// Request is a JSON-RPC 2.0 request (the MCP wire format).
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Error is a JSON-RPC error object.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Result is the canonical MCP result envelope.
type Result struct {
	Content []Content `json:"content"`
}

// Content is an MCP content block.
type Content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Tool is a narrow MCP tool.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     func(ctx context.Context, params json.RawMessage) (any, error)
}

// Server dispatches JSON-RPC methods ("initialize", "tools/list",
// "tools/call") to registered tools.
type Server struct {
	tools map[string]*Tool
}

func NewServer() *Server {
	return &Server{tools: map[string]*Tool{}}
}

// Register adds a narrow tool. Each tool is independently verifiable.
func (s *Server) Register(t *Tool) {
	s.tools[t.Name] = t
}

// Handle processes one JSON-RPC message and returns the response bytes.
func (s *Server) Handle(ctx context.Context, body []byte) ([]byte, error) {
	var req Request

	if err := json.Unmarshal(body, &req); err != nil {
		return json.Marshal(Response{
			JSONRPC: "2.0",
			Error:   &Error{Code: -32700, Message: "parse error"},
		})
	}

	id := req.ID

	switch req.Method {
	case "initialize":
		return json.Marshal(Response{
			JSONRPC: "2.0",
			ID:      id,
			Result: Result{Content: []Content{
				{Type: "text", Text: "CommerceOS MCP server initialized"},
			}},
		})

	case "tools/list":
		return s.listTools(id)

	case "tools/call":
		return s.callTool(ctx, id, req.Params)

	default:
		return json.Marshal(Response{
			JSONRPC: "2.0",
			ID:      id,
			Error:   &Error{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)},
		})
	}
}

func (s *Server) listTools(id json.RawMessage) ([]byte, error) {
	tools := make([]map[string]any, 0, len(s.tools))
	for _, t := range s.tools {
		tools = append(tools, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		})
	}

	return json.Marshal(Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  map[string]any{"tools": tools},
	})
}

func (s *Server) callTool(ctx context.Context, id json.RawMessage, params json.RawMessage) ([]byte, error) {
	var call struct {
		Name   string          `json:"name"`
		Params json.RawMessage `json:"arguments"`
	}

	if err := json.Unmarshal(params, &call); err != nil || call.Name == "" {
		return json.Marshal(Response{
			JSONRPC: "2.0",
			ID:      id,
			Error:   &Error{Code: -32602, Message: "invalid tool call params"},
		})
	}

	tool, ok := s.tools[call.Name]
	if !ok {
		return json.Marshal(Response{
			JSONRPC: "2.0",
			ID:      id,
			Error:   &Error{Code: -32601, Message: fmt.Sprintf("unknown tool: %s", call.Name)},
		})
	}

	result, err := tool.Handler(ctx, call.Params)
	if err != nil {
		return json.Marshal(Response{
			JSONRPC: "2.0",
			ID:      id,
			Error:   &Error{Code: 1, Message: err.Error()},
		})
	}

	return json.Marshal(Response{
		JSONRPC: "2.0",
		ID:      id,
		Result: Result{Content: []Content{
			{Type: "text", Text: toText(result)},
		}},
	})
}

func toText(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
