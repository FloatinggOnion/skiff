// Package mcp serves tools over the Model Context Protocol on stdio: newline-delimited JSON-RPC 2.0.
package mcp

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
)

const protocolVersion = "2025-06-18"

type Tool struct {
	Name        string                                    `json:"name"`
	Description string                                    `json:"description"`
	InputSchema map[string]any                            `json:"inputSchema"`
	Run         func(args map[string]any) (string, error) `json:"-"`
}

type Server struct {
	Name    string
	Version string
	Tools   []Tool
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve answers requests from in until it closes; tool calls run concurrently so a slow deploy doesn't block a ping.
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	var mu sync.Mutex
	enc := json.NewEncoder(out)
	send := func(r response) {
		mu.Lock()
		defer mu.Unlock()
		_ = enc.Encode(r)
	}
	var wg sync.WaitGroup
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 8<<20)
	for sc.Scan() {
		var req request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			send(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
			continue
		}
		if len(req.ID) == 0 {
			continue // a notification: nothing to answer
		}
		wg.Add(1)
		go func(req request) {
			defer wg.Done()
			result, err := s.handle(req)
			if err != nil {
				send(response{JSONRPC: "2.0", ID: req.ID, Error: err})
				return
			}
			send(response{JSONRPC: "2.0", ID: req.ID, Result: result})
		}(req)
	}
	wg.Wait()
	return sc.Err()
}

func (s *Server) handle(req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		v := p.ProtocolVersion
		if v == "" {
			v = protocolVersion
		}
		return map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]string{"name": s.Name, "version": s.Version},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.Tools}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if json.Unmarshal(req.Params, &p) != nil {
			return nil, &rpcError{-32602, "invalid params"}
		}
		for _, t := range s.Tools {
			if t.Name != p.Name {
				continue
			}
			text, err := t.Run(p.Arguments)
			if err != nil {
				return toolResult(err.Error(), true), nil
			}
			return toolResult(text, false), nil
		}
		return nil, &rpcError{-32602, "unknown tool " + p.Name}
	}
	return nil, &rpcError{-32601, "method not found: " + req.Method}
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]string{{"type": "text", "text": text}},
		"isError": isError,
	}
}
