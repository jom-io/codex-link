package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jom-io/codex-link/internal/link"
)

func TestMCPPermissionsAndSessionOwnership(t *testing.T) {
	var calls int
	s := mcpServer{home: "test-home", session: "window-one", harness: "deepseek-harness", call: func(_ context.Context, home, op string, a link.APIRequest) (json.RawMessage, error) {
		calls++
		if home != "test-home" || a.Session != "window-one" {
			t.Fatal("session or home changed", home, a.Session)
		}
		if op != "task/claim" || a.ID != "exact-task-id" || a.Lease != 900 {
			t.Fatal("incorrect daemon operation", op, a)
		}
		return json.RawMessage(`{"status":"claimed"}`), nil
	}}
	if _, e := s.toolCall(context.Background(), "agent_link_send", json.RawMessage(`{"to":"peer","text":"hello"}`)); e == nil || calls != 0 {
		t.Fatal("read-only adapter exposed writes")
	}
	s.write = true
	for _, args := range []string{`{"id":"exact-task-id","session":"other-window"}`, `{"id":"exact-task-id","lease":null}`, `{"id":"exact-task-id","lease":1}`, `{"id":"exact-task-id","lease":900.5}`, `{}`, `null`} {
		if _, e := s.toolCall(context.Background(), "agent_link_task_claim", json.RawMessage(args)); e == nil {
			t.Fatal("invalid or spoofed claim accepted", args)
		}
	}
	if calls != 0 {
		t.Fatal("invalid arguments reached daemon")
	}
	if _, e := s.toolCall(context.Background(), "agent_link_task_claim", json.RawMessage(`{"id":"exact-task-id"}`)); e != nil || calls != 1 {
		t.Fatal(e, calls)
	}
	if _, e := s.toolCall(context.Background(), "agent_link_file_send", json.RawMessage(`{"to":"peer","path":"/selected"}`)); e == nil || calls != 1 {
		t.Fatal("file access enabled without opt-in")
	}
	s.files = true
	if _, e := s.toolCall(context.Background(), "agent_link_file_send", json.RawMessage(`{"to":"peer","path":"relative"}`)); e == nil || calls != 1 {
		t.Fatal("relative file path accepted")
	}
	for _, name := range []string{"agent_link_shell", "agent_link_pair_accept", "agent_link_daemon_stop", "agent_link_configure"} {
		if _, e := s.toolCall(context.Background(), name, nil); e == nil {
			t.Fatal("privileged tool exposed", name)
		}
	}
}

func TestMCPStdioLifecycleAndErrors(t *testing.T) {
	s := mcpServer{session: "window-two", harness: "claude-code", call: func(_ context.Context, _, _ string, _ link.APIRequest) (json.RawMessage, error) {
		return nil, errors.New("daemon unavailable")
	}}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":0,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"agent_link_context","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"agent_link_status","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":5,"method":"unsupported"}`,
		`not-json`,
	}, "\n")
	var out bytes.Buffer
	if e := s.serve(strings.NewReader(input), &out); e != nil {
		t.Fatal(e)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 7 {
		t.Fatal("notification was answered or stdout polluted", out.String())
	}
	var responses []map[string]any
	for _, line := range lines {
		var r map[string]any
		if e := json.Unmarshal([]byte(line), &r); e != nil || r["jsonrpc"] != "2.0" {
			t.Fatal("invalid JSON-RPC stdout", line)
		}
		responses = append(responses, r)
	}
	if responses[0]["error"] == nil || responses[1]["result"].(map[string]any)["protocolVersion"] != "2025-06-18" {
		t.Fatal("lifecycle negotiation failed")
	}
	result := responses[2]["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, `"session":"window-two"`) || !strings.Contains(text, `"harness":"claude-code"`) {
		t.Fatal("context did not report actual session", text)
	}
	if responses[3]["result"].(map[string]any)["isError"] != true {
		t.Fatal("daemon failure not reported as tool error")
	}
	tools := responses[4]["result"].(map[string]any)["tools"].([]any)
	for _, tool := range tools {
		if tool.(map[string]any)["annotations"].(map[string]any)["readOnlyHint"] != true {
			t.Fatal("read-only tool list contains a mutation")
		}
	}
	if responses[5]["error"].(map[string]any)["code"] != float64(-32601) || responses[6]["error"].(map[string]any)["code"] != float64(-32700) {
		t.Fatal("wrong protocol error codes")
	}
}

func TestNamedAgentsHaveUniqueLeaseOwners(t *testing.T) {
	for _, agent := range []string{"office-codex", "home-codex", "home-ds"} {
		a, e := newSession(agent)
		b, e2 := newSession(agent)
		if e != nil || e2 != nil || a == b || !link.ValidLabel(a) || !strings.HasPrefix(a, agent+"-") {
			t.Fatal("agent name must not collapse concurrent windows into one owner", agent, a, b)
		}
	}
	for _, bad := range []string{"bad agent", strings.Repeat("x", 32), "../../agent"} {
		if _, e := newSession(bad); e == nil {
			t.Fatal("invalid agent accepted", bad)
		}
	}
}

func TestMCPSharedHostAllocatesDistinctChatSessions(t *testing.T) {
	var owners []string
	s := mcpServer{session: "shared-host", agent: "home-ds", write: true, call: func(_ context.Context, _, op string, a link.APIRequest) (json.RawMessage, error) {
		owners = append(owners, a.Session)
		return json.RawMessage(`{"status":"claimed"}`), nil
	}}
	var allocated []string
	for i := 0; i < 2; i++ {
		result, e := s.toolCall(context.Background(), "agent_link_worker_new", json.RawMessage(`{}`))
		if e != nil {
			t.Fatal(e)
		}
		session := result.(map[string]string)["session"]
		allocated = append(allocated, session)
		args, _ := json.Marshal(map[string]string{"id": "same-task", "session": session})
		if _, e = s.toolCall(context.Background(), "agent_link_task_claim", args); e != nil {
			t.Fatal(e)
		}
	}
	if owners[0] == owners[1] || !strings.HasPrefix(owners[0], "home-ds-") {
		t.Fatal("shared MCP host collapsed lease owners", owners)
	}
	// A fresh MCP process must explicitly resume the old owner at startup.
	other := mcpServer{session: "other-host", write: true}
	args, _ := json.Marshal(map[string]string{"id": "same-task", "session": allocated[0]})
	if _, e := other.toolCall(context.Background(), "agent_link_task_claim", args); e == nil {
		t.Fatal("unissued session accepted by another process")
	}
}
