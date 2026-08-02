package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/blacklotus88888/knowledge-service/internal/store"
)

// supportedVersions lists protocol versions this server can speak, newest first.
var supportedVersions = []string{"2025-03-26", "2024-11-05"}

// Server implements an MCP (Model Context Protocol) JSON-RPC 2.0 server over stdio.
type Server struct {
	store    *store.Store
	logger   *slog.Logger
	docsPath string // if non-empty, write_knowledge persists to .md files here
	version  string
}

// NewServer creates a Server. docsPath, when non-empty, activates markdown-as-source-of-truth
// mode: write_knowledge writes .md files to docsPath and re-ingests instead of writing to the DB directly.
// version is the binary's release version (e.g. "v0.2.3"), injected via -ldflags at build time.
func NewServer(s *store.Store, logger *slog.Logger, docsPath, version string) *Server {
	return &Server{store: s, logger: logger, docsPath: docsPath, version: version}
}

// Run reads JSON-RPC messages from r and writes responses to w until EOF.
// Used by the stdio transport (Claude Code, OpenCode).
func (srv *Server) Run(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4<<20), 4<<20) // 4 MB max message
	enc := json.NewEncoder(w)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			srv.logger.Debug("malformed message", "err", err)
			continue
		}
		srv.handle(enc, &msg)
	}
	return scanner.Err()
}

// HandleRequest processes a single JSON-RPC message and writes the response to w.
// Used by the HTTP transport so any HTTP client can send one-shot requests.
func (srv *Server) HandleRequest(body []byte, w io.Writer) {
	enc := json.NewEncoder(w)
	var msg rpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		srv.replyErr(enc, nil, -32700, "parse error")
		return
	}
	srv.handle(enc, &msg)
}

// OpenAITools returns the tool list in OpenAI function-calling format.
// Any model that uses the OpenAI API (GPT-4o, Gemini with OpenAI compat, local Ollama, etc.)
// can consume this to call the REST endpoints.
func OpenAITools() []map[string]any {
	var out []map[string]any
	for _, t := range toolsList() {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t["name"],
				"description": t["description"],
				"parameters":  t["inputSchema"],
			},
		})
	}
	return out
}

// --- internal types ---

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (srv *Server) reply(enc *json.Encoder, id json.RawMessage, result any) {
	_ = enc.Encode(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func (srv *Server) replyErr(enc *json.Encoder, id json.RawMessage, code int, msg string) {
	_ = enc.Encode(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

// --- dispatch ---

func (srv *Server) handle(enc *json.Encoder, msg *rpcMessage) {
	srv.logger.Debug("rpc", "method", msg.Method)

	switch msg.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(msg.Params, &params)
		version := negotiateVersion(params.ProtocolVersion)
		srv.reply(enc, msg.ID, map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "knowledge-service", "version": srv.version},
		})

	case "notifications/initialized", "initialized":
		// Notifications have no id and expect no response.

	case "tools/list":
		srv.reply(enc, msg.ID, map[string]any{"tools": toolsList()})

	case "tools/call":
		srv.handleToolCall(enc, msg)

	case "resources/list":
		srv.reply(enc, msg.ID, map[string]any{"resources": []any{}})

	case "prompts/list":
		srv.reply(enc, msg.ID, map[string]any{"prompts": []any{}})

	case "ping":
		srv.reply(enc, msg.ID, map[string]any{})

	default:
		if msg.ID != nil {
			srv.replyErr(enc, msg.ID, -32601, fmt.Sprintf("method not found: %s", msg.Method))
		}
	}
}

// --- tools ---

func toolsList() []map[string]any {
	return []map[string]any{
		{
			"name":        "search_knowledge",
			"description": "Search the knowledge base for relevant documentation, runbooks, solutions, and context. Call this before answering any question where prior knowledge might exist — infrastructure, architecture, debugging, procedures, or past incidents. If results are weak or absent, answer from your training and offer to save the solution afterward.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Natural language search query",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Max results (default 5, max 20)",
						"default":     5,
					},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "list_knowledge",
			"description": "List all documents and section headings in the knowledge base. Use this to discover what exists before writing a new entry (to avoid duplicates) or to find the exact path needed for delete_knowledge.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"filter": map[string]any{
						"type":        "string",
						"description": "Optional path prefix to filter by, e.g. 'solutions/' or 'runbooks/'",
					},
				},
			},
		},
		{
			"name":        "write_knowledge",
			"description": "Persist a knowledge entry as a markdown file and index it for future search. Call this after solving a non-trivial problem, completing an incident, or discovering something non-obvious — so future sessions can find it. Do NOT write ephemeral conversation state, user preferences, or information that is version-specific and will expire quickly. Each call adds or updates one section (## heading) inside the target file; existing sections in the same file are preserved. IMPORTANT: scripts and one-liners intended for get_tool must be stored under a tools/ path (e.g. tools/drain-node.md) — only that prefix is searched by get_tool.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Relative path inside docs/. Use runbooks/<service>-<symptom>.md, solutions/<topic>.md, guides/<topic>.md, or tools/<name>.md for scripts retrievable via get_tool.",
					},
					"heading": map[string]any{
						"type":        "string",
						"description": "Section heading (## level). Used as the primary search anchor. Be specific: 'OOMKilled on argocd-server' beats 'Problem'.",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "Markdown body. Include: symptom, root cause, exact fix commands, and links. Scripts must be in fenced code blocks to be retrievable via get_tool.",
					},
				},
				"required": []string{"path", "heading", "content"},
			},
		},
		{
			"name":        "delete_knowledge",
			"description": "Permanently delete a document from the knowledge base (removes the .md file from disk and all index entries). This action is irreversible — there is no undo. Use only when a runbook is dangerously wrong, completely obsolete, or duplicated by a better entry. Prefer write_knowledge to update outdated sections. Always call list_knowledge first to confirm the exact path.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Exact path of the document to delete as shown by list_knowledge (e.g. runbooks/argocd-oom.md). The .md extension is optional.",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			"name":        "get_tool",
			"description": "Retrieve a stored command, script, or kubectl one-liner by name. Returns raw executable code ready to run. Only searches documents stored under the tools/ path prefix — use write_knowledge with a tools/<name>.md path to add new tools. For general documentation, use search_knowledge instead.",
			"inputSchema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Tool name or description, e.g. 'drain node', 'restart argocd', 'check disk pressure'",
					},
				},
				"required": []string{"name"},
			},
		},
	}
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (srv *Server) handleToolCall(enc *json.Encoder, msg *rpcMessage) {
	var p toolCallParams
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		srv.replyErr(enc, msg.ID, -32602, "invalid params")
		return
	}

	switch p.Name {
	case "search_knowledge":
		srv.toolSearch(enc, msg.ID, p.Arguments)
	case "list_knowledge":
		srv.toolList(enc, msg.ID, p.Arguments)
	case "write_knowledge":
		srv.toolWrite(enc, msg.ID, p.Arguments)
	case "delete_knowledge":
		srv.toolDelete(enc, msg.ID, p.Arguments)
	case "get_tool":
		srv.toolGetTool(enc, msg.ID, p.Arguments)
	default:
		srv.replyErr(enc, msg.ID, -32602, fmt.Sprintf("unknown tool: %s", p.Name))
	}
}

type searchArgs struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func (srv *Server) toolSearch(enc *json.Encoder, id json.RawMessage, raw json.RawMessage) {
	var args searchArgs
	if err := json.Unmarshal(raw, &args); err != nil || args.Query == "" {
		srv.replyErr(enc, id, -32602, "query is required")
		return
	}
	if args.Limit <= 0 {
		args.Limit = 5
	}
	if args.Limit > 20 {
		args.Limit = 20
	}

	results, err := store.Hybrid(srv.store, args.Query, args.Limit, false)
	if err != nil {
		srv.logger.Error("search failed", "err", err)
		srv.replyErr(enc, id, -32603, "search error")
		return
	}

	text := formatResults(results, args.Query)
	srv.reply(enc, id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	})
}

type writeArgs struct {
	Path    string `json:"path"`
	Heading string `json:"heading"`
	Content string `json:"content"`
}

func (srv *Server) toolWrite(enc *json.Encoder, id json.RawMessage, raw json.RawMessage) {
	var args writeArgs
	if err := json.Unmarshal(raw, &args); err != nil || args.Path == "" || args.Heading == "" || args.Content == "" {
		srv.replyErr(enc, id, -32602, "path, heading, and content are all required")
		return
	}

	if srv.docsPath != "" {
		// Normalize path — always store with .md so list_knowledge/delete_knowledge paths stay consistent.
		mdPath := args.Path
		if !strings.HasSuffix(mdPath, ".md") {
			mdPath += ".md"
		}
		filePath, err := safeFilePath(srv.docsPath, mdPath)
		if err != nil {
			srv.replyErr(enc, id, -32602, "invalid path: "+err.Error())
			return
		}

		// Markdown-as-source-of-truth: write/update the .md file on disk, then re-ingest.
		// The DB is a derived index — the .md file is the canonical record.
		if err := upsertMarkdownSection(filePath, args.Heading, args.Content); err != nil {
			srv.logger.Error("write markdown failed", "path", filePath, "err", err)
			srv.replyErr(enc, id, -32603, "write error")
			return
		}
		if err := store.Ingest(srv.store, srv.docsPath); err != nil {
			srv.logger.Error("ingest failed after markdown write", "err", err)
			srv.replyErr(enc, id, -32603, "ingest error")
			return
		}
		srv.reply(enc, id, map[string]any{
			"content": []map[string]any{{
				"type": "text",
				"text": fmt.Sprintf("Saved to knowledge base: %s / %s", mdPath, args.Heading),
			}},
		})
	} else {
		if err := srv.store.WriteChunk(args.Path, args.Heading, args.Content); err != nil {
			srv.logger.Error("write failed", "err", err)
			srv.replyErr(enc, id, -32603, "write error")
			return
		}
		srv.reply(enc, id, map[string]any{
			"content": []map[string]any{{
				"type": "text",
				"text": fmt.Sprintf("Saved to knowledge base: %s / %s", args.Path, args.Heading),
			}},
		})
	}
}

// upsertMarkdownSection writes or updates a "## heading" section in the markdown file at filePath.
// Parent directories and the file itself are created if they don't exist.
func upsertMarkdownSection(filePath, heading, content string) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0o750); err != nil {
		return err
	}
	existing, err := os.ReadFile(filePath) //nolint:gosec // filePath validated by safeFilePath before reaching here
	if err != nil {
		// New file: derive a title from the filename.
		stem := strings.TrimSuffix(filepath.Base(filePath), ".md")
		title := strings.ReplaceAll(stem, "-", " ")
		text := "# " + title + "\n\n## " + heading + "\n\n" + strings.TrimSpace(content) + "\n"
		return os.WriteFile(filePath, []byte(text), 0o600) //nolint:gosec // path validated by safeFilePath
	}
	return os.WriteFile(filePath, []byte(updateSection(string(existing), heading, content)), 0o600) //nolint:gosec // path validated by safeFilePath
}

// updateSection finds "## heading" in md and replaces its body with content.
// If the heading is absent, appends a new section at the end.
func updateSection(md, heading, content string) string {
	target := "## " + heading
	lines := strings.Split(md, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimRight(line, " ") == target {
			start = i
			break
		}
	}
	if start == -1 {
		return strings.TrimRight(md, "\n") + "\n\n## " + heading + "\n\n" + strings.TrimSpace(content) + "\n"
	}
	// Find the end of this section: any heading at any level stops the section,
	// so ### sub-headings are preserved rather than silently overwritten.
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if isHeadingLine(lines[i]) {
			end = i
			break
		}
	}
	var out []string
	out = append(out, lines[:start]...)
	out = append(out, target, "")
	out = append(out, strings.Split(strings.TrimSpace(content), "\n")...)
	out = append(out, "")
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

type listArgs struct {
	Filter string `json:"filter"`
}

func (srv *Server) toolList(enc *json.Encoder, id json.RawMessage, raw json.RawMessage) {
	var args listArgs
	_ = json.Unmarshal(raw, &args)

	docs, err := srv.store.ListDocuments(args.Filter)
	if err != nil {
		srv.logger.Error("list failed", "err", err)
		srv.replyErr(enc, id, -32603, "list error")
		return
	}

	var sb strings.Builder
	if len(docs) == 0 {
		if args.Filter != "" {
			fmt.Fprintf(&sb, "No documents matching prefix %q.\n\nTry list_knowledge with no filter to see all entries, or check the path prefix.\n", args.Filter)
		} else {
			sb.WriteString("Knowledge base is empty.\n\nAdd entries with write_knowledge:\n  - runbooks/<service>-<symptom>.md  — incident runbooks\n  - solutions/<topic>.md             — one-time fixes with context\n  - tools/<name>.md                  — scripts (retrievable via get_tool)\n  - guides/<topic>.md                — how-to guides\n")
		}
	} else {
		total := 0
		for _, d := range docs {
			total += len(d.Headings)
		}
		fmt.Fprintf(&sb, "Knowledge base: %d documents, %d sections\n\n", len(docs), total)
		for _, d := range docs {
			fmt.Fprintf(&sb, "%s\n", d.Path)
			for _, h := range d.Headings {
				if h != "" {
					fmt.Fprintf(&sb, "  - %s\n", h)
				}
			}
		}
	}

	srv.reply(enc, id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": sb.String()}},
	})
}

type deleteArgs struct {
	Path string `json:"path"`
}

func (srv *Server) toolDelete(enc *json.Encoder, id json.RawMessage, raw json.RawMessage) {
	var args deleteArgs
	if err := json.Unmarshal(raw, &args); err != nil || args.Path == "" {
		srv.replyErr(enc, id, -32602, "path is required")
		return
	}

	// In markdown mode, remove the .md file from disk so it isn't re-ingested on next startup.
	docPath := args.Path
	if srv.docsPath != "" {
		mdPath := docPath
		if !strings.HasSuffix(mdPath, ".md") {
			mdPath += ".md"
		}
		docPath = mdPath
		filePath, err := safeFilePath(srv.docsPath, mdPath)
		if err != nil {
			srv.replyErr(enc, id, -32602, "invalid path: "+err.Error())
			return
		}
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			// Hard error: if the file remains on disk, Ingest will resurrect it on next startup.
			srv.logger.Error("could not remove markdown file", "path", filePath, "err", err)
			srv.replyErr(enc, id, -32603, "delete error: could not remove file from disk")
			return
		}
	}

	n, err := srv.store.DeleteDocument(docPath)
	if err != nil {
		srv.logger.Error("delete failed", "path", args.Path, "err", err)
		srv.replyErr(enc, id, -32603, err.Error())
		return
	}

	srv.reply(enc, id, map[string]any{
		"content": []map[string]any{{
			"type": "text",
			"text": fmt.Sprintf("Deleted: %s (%d sections removed)", docPath, n),
		}},
	})
}

type getToolArgs struct {
	Name string `json:"name"`
}

func (srv *Server) toolGetTool(enc *json.Encoder, id json.RawMessage, raw json.RawMessage) {
	var args getToolArgs
	if err := json.Unmarshal(raw, &args); err != nil || args.Name == "" {
		srv.replyErr(enc, id, -32602, "name is required")
		return
	}

	// Search with a wider limit so we have enough candidates after filtering to tools/.
	candidates, err := store.Hybrid(srv.store, args.Name, 10, false)
	if err != nil {
		srv.replyErr(enc, id, -32603, "search error")
		return
	}

	// Only return results stored under tools/ — other paths are runbooks/docs, not tools.
	var results []store.Result
	for _, r := range candidates {
		if strings.HasPrefix(r.Path, "tools/") {
			results = append(results, r)
		}
	}

	if len(results) == 0 {
		srv.reply(enc, id, map[string]any{
			"content": []map[string]any{{
				"type": "text",
				"text": fmt.Sprintf("No tool found for %q. Store tools under tools/<name>.md with ## headings and ```code blocks```.", args.Name),
			}},
		})
		return
	}

	best := results[0]
	code := extractCodeBlock(best.Content)
	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s\nSource: %s\n\n", best.Heading, best.Path)
	if code != best.Content {
		fmt.Fprintf(&sb, "```\n%s\n```", code)
	} else {
		sb.WriteString(best.Content)
	}

	srv.reply(enc, id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": sb.String()}},
	})
}

// extractCodeBlock returns the content of the first fenced code block in s,
// or s itself if no code block is found.
func extractCodeBlock(s string) string {
	lines := strings.Split(s, "\n")
	var inBlock bool
	var out []string
	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			if !inBlock {
				inBlock = true
				continue // skip the opening ``` line
			}
			break // end of block
		}
		if inBlock {
			out = append(out, line)
		}
	}
	if len(out) > 0 {
		return strings.TrimSpace(strings.Join(out, "\n"))
	}
	return s
}

// safeFilePath resolves relPath under docsPath and confirms the result stays
// inside docsPath, preventing path traversal via "../.." in user-supplied paths.
func safeFilePath(docsPath, relPath string) (string, error) {
	abs, err := filepath.Abs(filepath.Join(docsPath, relPath))
	if err != nil {
		return "", err
	}
	root, err := filepath.Abs(docsPath)
	if err != nil {
		return "", err
	}
	if abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the docs directory", relPath)
	}
	return abs, nil
}

// isHeadingLine reports whether line is a Markdown heading at any level (#, ##, ###, …).
func isHeadingLine(line string) bool {
	i := 0
	for i < len(line) && line[i] == '#' {
		i++
	}
	return i > 0 && i < len(line) && line[i] == ' '
}

// negotiateVersion returns the best version the server supports given the
// client's preferred version. Defaults to the server's oldest stable version.
func negotiateVersion(clientVersion string) string {
	for _, v := range supportedVersions {
		if v == clientVersion {
			return v
		}
	}
	return supportedVersions[len(supportedVersions)-1]
}

const maxPreviewChars = 1500

// scoreLabel converts an RRF score to a human-readable match quality label.
// Thresholds are calibrated to the wFTS=4.0 / wVec=0.5 / k=60 RRF parameters.
func scoreLabel(score float64) string {
	switch {
	case score >= 0.04:
		return "strong match"
	case score >= 0.015:
		return "relevant"
	default:
		return "weak match"
	}
}

func formatResults(results []store.Result, query string) string {
	if len(results) == 0 {
		return fmt.Sprintf(
			"No results found for: %q\n\nTip: use list_knowledge to see what topics exist, try broader or different terms, or use write_knowledge to add new content.",
			query,
		)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Knowledge base results for %q:\n\n", query)
	for i, r := range results {
		fmt.Fprintf(&sb, "--- [%d] %s", i+1, r.Path)
		if r.Heading != "" {
			fmt.Fprintf(&sb, " / %s", r.Heading)
		}
		fmt.Fprintf(&sb, " [%s] ---\n", scoreLabel(r.Score))
		preview := r.Content
		if len(preview) > maxPreviewChars {
			cut := maxPreviewChars
			if idx := strings.LastIndexByte(preview[:cut], ' '); idx > cut-60 {
				cut = idx
			}
			preview = preview[:cut] + "\n[...truncated — search with narrower terms for full context]"
		}
		fmt.Fprintf(&sb, "%s\n\n", preview)
	}
	return sb.String()
}
