package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
)

// ToolHandler is the function signature for MCP tool implementations.
type ToolHandler func(ctx context.Context, params map[string]any) (string, error)

// ToolDef defines an MCP tool with its schema and handler.
type ToolDef struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     ToolHandler
}

// MCPServer implements the Model Context Protocol server.
type MCPServer struct {
	name    string
	version string
	tools   map[string]ToolDef
	mu      sync.RWMutex
}

// JSON-RPC types
type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewMCPServer(name, version string) *MCPServer {
	return &MCPServer{
		name:    name,
		version: version,
		tools:   make(map[string]ToolDef),
	}
}

func (s *MCPServer) RegisterTool(t ToolDef) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[t.Name] = t
	log.Printf("  Registered tool: %s", t.Name)
}

func (s *MCPServer) ToolCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tools)
}

// handleRequest processes a single JSON-RPC request.
func (s *MCPServer) handleRequest(ctx context.Context, req jsonRPCRequest) jsonRPCResponse {
	switch req.Method {

	case "initialize":
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]any{
					"tools": map[string]any{
						"listChanged": false,
					},
				},
				"serverInfo": map[string]any{
					"name":    s.name,
					"version": s.version,
				},
			},
		}

	case "notifications/initialized":
		// No response needed for notifications
		return jsonRPCResponse{}

	case "tools/list":
		s.mu.RLock()
		defer s.mu.RUnlock()
		toolList := make([]map[string]any, 0, len(s.tools))
		for _, t := range s.tools {
			toolList = append(toolList, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
			})
		}
		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"tools": toolList,
			},
		}

	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return errorResponse(req.ID, -32602, "Invalid params: "+err.Error())
		}

		s.mu.RLock()
		tool, ok := s.tools[params.Name]
		s.mu.RUnlock()
		if !ok {
			return errorResponse(req.ID, -32601, fmt.Sprintf("Unknown tool: %s", params.Name))
		}

		result, err := tool.Handler(ctx, params.Arguments)
		if err != nil {
			return jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": fmt.Sprintf("Error: %v", err)},
					},
					"isError": true,
				},
			}
		}

		return jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": result},
				},
			},
		}

	case "ping":
		return jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}

	default:
		return errorResponse(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
	}
}

func errorResponse(id any, code int, msg string) jsonRPCResponse {
	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   mcpError{Code: code, Message: msg},
	}
}

// ServeStdio runs the MCP server over stdin/stdout (default for Claude Desktop).
func (s *MCPServer) ServeStdio() error {
	scanner := bufio.NewScanner(os.Stdin)
	// Increase buffer for large responses
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	writer := json.NewEncoder(os.Stdout)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req jsonRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			log.Printf("Failed to parse request: %v", err)
			continue
		}

		resp := s.handleRequest(context.Background(), req)

		// Don't respond to notifications (no ID)
		if req.ID == nil && req.Method == "notifications/initialized" {
			continue
		}

		if err := writer.Encode(resp); err != nil {
			log.Printf("Failed to write response: %v", err)
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		return fmt.Errorf("stdin read error: %w", err)
	}
	return nil
}

// ServeHTTP runs the MCP server as an HTTP endpoint (Streamable HTTP transport).
func (s *MCPServer) ServeHTTP(addr string) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		resp := s.handleRequest(r.Context(), req)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	log.Printf("HTTP server listening on %s", addr)
	return http.ListenAndServe(addr, mux)
}
