package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestServe(t *testing.T) {
	s := &Server{Name: "skiff", Version: "test", Tools: []Tool{
		{Name: "echo", InputSchema: map[string]any{"type": "object"}, Run: func(a map[string]any) (string, error) {
			return "hi " + a["who"].(string), nil
		}},
		{Name: "boom", Run: func(map[string]any) (string, error) { return "", errors.New("it broke") }},
	}}
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"who":"agent"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"boom"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"nope"}`,
		`not json`,
	}, "\n")
	var out strings.Builder
	if err := s.Serve(strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	byID := map[string]map[string]any{}
	sc := bufio.NewScanner(strings.NewReader(out.String()))
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("bad line %q", sc.Text())
		}
		id, _ := json.Marshal(m["id"])
		byID[string(id)] = m
	}
	if len(byID) != 6 {
		t.Fatalf("expected 6 responses (the notification gets none), got %d:\n%s", len(byID), out.String())
	}
	if v := byID["1"]["result"].(map[string]any)["protocolVersion"]; v != "2025-03-26" {
		t.Errorf("negotiated %v", v)
	}
	tools := byID["2"]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 2 || tools[0].(map[string]any)["name"] != "echo" {
		t.Errorf("tools %v", tools)
	}
	text := func(id string) (string, bool) {
		r := byID[id]["result"].(map[string]any)
		return r["content"].([]any)[0].(map[string]any)["text"].(string), r["isError"].(bool)
	}
	if got, isErr := text("3"); got != "hi agent" || isErr {
		t.Errorf("echo: %q %v", got, isErr)
	}
	if got, isErr := text("4"); got != "it broke" || !isErr {
		t.Errorf("tool error should come back as a result with isError: %q %v", got, isErr)
	}
	if byID["5"]["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Error("unknown method should be -32601")
	}
	if byID["null"]["error"].(map[string]any)["code"].(float64) != -32700 {
		t.Error("garbage should be a parse error")
	}
}
