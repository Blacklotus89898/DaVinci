package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blacklotus88888/knowledge-service/internal/store"
)

// --- helpers ---

func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "mcp_test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewServer(s, logger, ""), s
}

// exchange sends one JSON-RPC request and returns the decoded response.
func exchange(t *testing.T, srv *Server, req string) map[string]any {
	t.Helper()
	var out bytes.Buffer
	in := strings.NewReader(req + "\n")
	if err := srv.Run(in, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	scanner := bufio.NewScanner(&out)
	var last map[string]any
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("unmarshal response %q: %v", line, err)
		}
		last = m
	}
	if last == nil {
		t.Fatalf("no response from server for request: %s", req)
	}
	return last
}

// exchangeAll returns ALL response lines for a sequence of messages.
func exchangeAll(t *testing.T, srv *Server, msgs ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	in := strings.NewReader(strings.Join(msgs, "\n") + "\n")
	if err := srv.Run(in, &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var responses []map[string]any
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		responses = append(responses, m)
	}
	return responses
}

// --- MCP protocol tests ---

func TestInitialize(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := exchange(t, srv, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","clientInfo":{"name":"test","version":"1"}}}`)

	if resp["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v", resp["jsonrpc"])
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("result is not an object: %v", resp["result"])
	}
	// Server should echo back the client's requested version.
	if result["protocolVersion"] != "2025-03-26" {
		t.Errorf("protocolVersion = %v, want 2025-03-26", result["protocolVersion"])
	}
	caps, _ := result["capabilities"].(map[string]any)
	if _, hasTools := caps["tools"]; !hasTools {
		t.Error("capabilities.tools missing")
	}
}

func TestInitializeFallback(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := exchange(t, srv, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	result := resp["result"].(map[string]any)
	// Unknown version: server falls back to its oldest supported version.
	if result["protocolVersion"] == "1999-01-01" {
		t.Error("server should not accept an unknown protocol version")
	}
}

func TestToolsList(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := exchange(t, srv, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("result not an object: %v", resp)
	}
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) < 2 {
		t.Fatalf("expected ≥2 tools, got: %v", result["tools"])
	}
	names := make(map[string]bool)
	for _, raw := range tools {
		tool := raw.(map[string]any)
		names[tool["name"].(string)] = true
	}
	for _, want := range []string{"search_knowledge", "write_knowledge"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestToolCallSearchEmpty(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := exchange(t, srv, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"search_knowledge","arguments":{"query":"anything"}}}`)

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result, got: %v", resp)
	}
	content := result["content"].([]any)
	if len(content) == 0 {
		t.Fatal("content array is empty")
	}
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "No results") {
		t.Errorf("expected 'No results' message, got: %q", text)
	}
}

func TestToolCallSearchHit(t *testing.T) {
	srv, s := newTestServer(t)

	// Seed data.
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "ops.md"), []byte(`# Ops
## ArgoCD OOM Fix
ArgoCD was OOM killed. Fix by deploying pod-cleanup CronJob every 15 minutes.
`), 0o644)
	if err := store.Ingest(s, dir); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	resp := exchange(t, srv, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"search_knowledge","arguments":{"query":"argocd oom fix","limit":3}}}`)

	result := resp["result"].(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if strings.Contains(text, "No results") {
		t.Errorf("expected a hit, got 'No results'; text=%q", text)
	}
	if !strings.Contains(strings.ToLower(text), "oom") {
		t.Errorf("expected OOM in results, got: %q", text)
	}
}

func TestToolCallWrite(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := exchange(t, srv, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"write_knowledge","arguments":{"path":"solutions/test.md","heading":"Test Fix","content":"Run kubectl rollout restart to fix test."}}}`)

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("write failed: %v", resp)
	}
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "Saved") {
		t.Errorf("expected 'Saved' confirmation, got: %q", text)
	}
}

func TestToolCallMissingQuery(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := exchange(t, srv, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"search_knowledge","arguments":{}}}`)
	if _, hasErr := resp["error"]; !hasErr {
		t.Errorf("expected error for missing query, got: %v", resp)
	}
}

func TestUnknownMethod(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := exchange(t, srv, `{"jsonrpc":"2.0","id":7,"method":"nonexistent/method"}`)
	if _, ok := resp["error"]; !ok {
		t.Errorf("expected error for unknown method, got: %v", resp)
	}
}

func TestNotificationNoResponse(t *testing.T) {
	srv, _ := newTestServer(t)
	// notifications/initialized has no id → must not receive a response
	responses := exchangeAll(t, srv,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":8,"method":"ping"}`,
	)
	// Only the ping should produce a response.
	if len(responses) != 1 {
		t.Errorf("expected 1 response (ping only), got %d: %v", len(responses), responses)
	}
}

func TestToolsList_FourTools(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := exchange(t, srv, `{"jsonrpc":"2.0","id":20,"method":"tools/list"}`)
	result := resp["result"].(map[string]any)
	tools := result["tools"].([]any)
	names := make(map[string]bool)
	for _, raw := range tools {
		names[raw.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"search_knowledge", "list_knowledge", "write_knowledge", "delete_knowledge"} {
		if !names[want] {
			t.Errorf("missing tool %q; got %v", want, names)
		}
	}
}

func TestToolCallListEmpty(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := exchange(t, srv, `{"jsonrpc":"2.0","id":21,"method":"tools/call","params":{"name":"list_knowledge","arguments":{}}}`)
	result := resp["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "empty") {
		t.Errorf("expected 'empty' for fresh DB, got: %q", text)
	}
}

func TestToolCallListAfterIngest(t *testing.T) {
	srv, s := newTestServer(t)
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "ops.md"), []byte("# Ops\n## ArgoCD Fix\nFix content.\n"), 0o644)
	_ = store.Ingest(s, dir)

	resp := exchange(t, srv, `{"jsonrpc":"2.0","id":22,"method":"tools/call","params":{"name":"list_knowledge","arguments":{}}}`)
	text := resp["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "ops.md") {
		t.Errorf("expected ops.md in list, got: %q", text)
	}
	if !strings.Contains(text, "ArgoCD Fix") {
		t.Errorf("expected heading in list, got: %q", text)
	}
}

func TestToolCallDelete(t *testing.T) {
	srv, s := newTestServer(t)
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "todelete.md"), []byte("# Delete Me\n## Section\nContent.\n"), 0o644)
	_ = store.Ingest(s, dir)

	// Verify it exists.
	resp := exchange(t, srv, `{"jsonrpc":"2.0","id":23,"method":"tools/call","params":{"name":"list_knowledge","arguments":{}}}`)
	text := resp["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "todelete.md") {
		t.Fatalf("expected todelete.md, got: %q", text)
	}

	// Delete it.
	resp = exchange(t, srv, `{"jsonrpc":"2.0","id":24,"method":"tools/call","params":{"name":"delete_knowledge","arguments":{"path":"todelete.md"}}}`)
	text = resp["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "Deleted") {
		t.Errorf("expected 'Deleted' confirmation, got: %q", text)
	}

	// Verify it's gone.
	resp = exchange(t, srv, `{"jsonrpc":"2.0","id":25,"method":"tools/call","params":{"name":"list_knowledge","arguments":{}}}`)
	text = resp["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if strings.Contains(text, "todelete.md") {
		t.Errorf("expected todelete.md to be gone, still in list: %q", text)
	}
}

func TestToolCallDeleteNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := exchange(t, srv, `{"jsonrpc":"2.0","id":26,"method":"tools/call","params":{"name":"delete_knowledge","arguments":{"path":"nonexistent.md"}}}`)
	if _, hasErr := resp["error"]; !hasErr {
		t.Errorf("expected error for nonexistent path, got: %v", resp)
	}
}

func TestSearchNoResultsTip(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := exchange(t, srv, `{"jsonrpc":"2.0","id":27,"method":"tools/call","params":{"name":"search_knowledge","arguments":{"query":"xyzzy nonexistent"}}}`)
	text := resp["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "Tip:") {
		t.Errorf("expected tip in no-results response, got: %q", text)
	}
}

func TestResourcesList(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := exchange(t, srv, `{"jsonrpc":"2.0","id":9,"method":"resources/list"}`)
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object: %v", resp)
	}
	if _, ok := result["resources"]; !ok {
		t.Error("missing 'resources' key in response")
	}
}

func TestPromptsList(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := exchange(t, srv, `{"jsonrpc":"2.0","id":10,"method":"prompts/list"}`)
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object: %v", resp)
	}
	if _, ok := result["prompts"]; !ok {
		t.Error("missing 'prompts' key in response")
	}
}

func TestToolCallWriteEmptyHeading(t *testing.T) {
	srv, _ := newTestServer(t)
	// heading is empty string — must be rejected server-side.
	resp := exchange(t, srv, `{"jsonrpc":"2.0","id":30,"method":"tools/call","params":{"name":"write_knowledge","arguments":{"path":"test/x.md","heading":"","content":"some content"}}}`)
	if _, hasErr := resp["error"]; !hasErr {
		t.Errorf("expected error for empty heading, got result: %v", resp)
	}
}

func TestGetToolFiltersToToolsPath(t *testing.T) {
	srv, s := newTestServer(t)

	// Write a runbook (not a tool) — should NOT be returned by get_tool.
	if err := s.WriteChunk("runbooks/drain-runbook.md", "Drain Node Runbook", "Use kubectl drain to evict pods."); err != nil {
		t.Fatalf("WriteChunk runbook: %v", err)
	}
	// Write an actual tool under tools/ — should be returned.
	if err := s.WriteChunk("tools/drain-node.md", "Drain Node Tool", "```bash\nkubectl drain <node> --ignore-daemonsets\n```"); err != nil {
		t.Fatalf("WriteChunk tool: %v", err)
	}

	resp := exchange(t, srv, `{"jsonrpc":"2.0","id":31,"method":"tools/call","params":{"name":"get_tool","arguments":{"name":"drain node"}}}`)
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result: %v", resp)
	}
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	// Must come from tools/, not the runbook.
	if strings.Contains(text, "runbooks/") {
		t.Errorf("get_tool returned a runbook path; should only return tools/: %q", text)
	}
	if !strings.Contains(text, "kubectl drain") {
		t.Errorf("expected kubectl drain command in tool result, got: %q", text)
	}
}

func TestGetToolEmptyKB(t *testing.T) {
	srv, _ := newTestServer(t)
	resp := exchange(t, srv, `{"jsonrpc":"2.0","id":32,"method":"tools/call","params":{"name":"get_tool","arguments":{"name":"anything"}}}`)
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result: %v", resp)
	}
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "No tool found") {
		t.Errorf("expected 'No tool found' for empty KB, got: %q", text)
	}
}

func TestNegotiateVersion(t *testing.T) {
	cases := []struct {
		client string
		want   string
	}{
		{"2025-03-26", "2025-03-26"},
		{"2024-11-05", "2024-11-05"},
		{"9999-01-01", "2024-11-05"}, // unknown → oldest stable
		{"", "2024-11-05"},           // empty → oldest stable
	}
	for _, c := range cases {
		got := negotiateVersion(c.client)
		if got != c.want {
			t.Errorf("negotiateVersion(%q) = %q, want %q", c.client, got, c.want)
		}
	}
}
