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

	"github.com/blacklotus88888/knowledge-service/internal/chunker"
	"github.com/blacklotus88888/knowledge-service/internal/lock"
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

// NewServer creates a Server. docsPath is the markdown source-of-truth directory:
// write_knowledge writes .md files there and re-ingests them. An empty docsPath
// disables all write/delete tools (search and list still work read-only).
// version is the binary's release version (e.g. "v0.2.3"), injected via -ldflags at build time.
func NewServer(s *store.Store, logger *slog.Logger, docsPath, version string) *Server {
	return &Server{store: s, logger: logger, docsPath: docsPath, version: version}
}

// lockPath returns the path of the cross-process write lock file inside docsPath.
func (srv *Server) lockPath() string {
	return filepath.Join(srv.docsPath, ".knowledge.lock")
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
			"annotations": map[string]any{"readOnlyHint": true},
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
					"path_prefix": map[string]any{
						"type":        "string",
						"description": "Optional path prefix to restrict results to, e.g. 'runbooks/' or 'tools/'",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "list_knowledge",
			"description": "List all documents and section headings in the knowledge base. Use this to discover what exists before writing a new entry (to avoid duplicates) or to find the exact path needed for delete_knowledge.",
			"annotations": map[string]any{"readOnlyHint": true},
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
			"description": "Persist a knowledge entry as a markdown file and index it for future search. Call this after solving a non-trivial problem, completing an incident, or discovering something non-obvious — so future sessions can find it. Do NOT write ephemeral conversation state, user preferences, or information that is version-specific and will expire quickly. Each call adds or updates one section (## heading) inside the target file; other sections in the same file are preserved. Use ### subsections inside content for structure (e.g. ### Symptom, ### Fix) — internal ## headings are demoted automatically. Scripts and one-liners intended for get_tool must be stored under a tools/ path (e.g. tools/drain-node.md), with the script in a fenced code block.",
			"annotations": map[string]any{"idempotentHint": true},
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
						"description": "Markdown body. Include: symptom, root cause, exact fix commands, and links. Use ### for internal structure; scripts go in fenced code blocks.",
					},
				},
				"required": []string{"path", "heading", "content"},
			},
		},
		{
			"name":        "delete_knowledge",
			"description": "Permanently delete a document from the knowledge base (removes the .md file from disk and all index entries). This action is irreversible — there is no undo. Use only when a runbook is dangerously wrong, completely obsolete, or duplicated by a better entry. Prefer write_knowledge to update outdated sections. Always call list_knowledge first to confirm the exact path.",
			"annotations": map[string]any{"destructiveHint": true},
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
			"description": "Retrieve a stored command, script, or kubectl one-liner by name. Returns raw executable code ready to run. The name is matched exactly against tools/<name>.md (case-insensitive, spaces become hyphens — 'drain node' finds tools/drain-node.md); a fuzzy search over tools/ is the fallback. Add tools with write_knowledge using a tools/<name>.md path containing a fenced code block. For general documentation, use search_knowledge instead.",
			"annotations": map[string]any{"readOnlyHint": true},
			"inputSchema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Tool name, e.g. 'drain node', 'restart argocd', 'check disk pressure'",
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
	Query      string `json:"query"`
	Limit      int    `json:"limit"`
	PathPrefix string `json:"path_prefix"`
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
	// Normalize the prefix so "tools" and "tools/" behave the same.
	if args.PathPrefix != "" && !strings.HasSuffix(args.PathPrefix, "/") {
		args.PathPrefix += "/"
	}

	results, err := store.Search(srv.store, store.SearchOpts{
		Query:      args.Query,
		Limit:      args.Limit,
		PathPrefix: args.PathPrefix,
	})
	if err != nil {
		srv.logger.Error("search failed", "err", err)
		srv.replyErr(enc, id, -32603, "search error")
		return
	}

	text := formatResults(results, args.Query, srv.store.HasProvider())
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
	if srv.docsPath == "" {
		srv.replyErr(enc, id, -32603, "server misconfigured: DOCS_PATH is not set — markdown files are the only source of truth")
		return
	}

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

	// Serialise against other knowledge-service processes writing the same docs tree.
	if err := os.MkdirAll(filepath.Dir(filePath), 0o750); err != nil {
		srv.replyErr(enc, id, -32603, "write error: "+err.Error())
		return
	}
	release, err := lock.Acquire(srv.lockPath())
	if err != nil {
		srv.logger.Error("write lock", "err", err)
		srv.replyErr(enc, id, -32603, "write error: "+err.Error())
		return
	}

	// Markdown-as-source-of-truth: write/update the .md file on disk, then re-ingest
	// just this file. The DB is a derived index — the .md file is the canonical record.
	// Internal ## headings are demoted to ### so they stay inside this section.
	err = upsertMarkdownSection(filePath, args.Heading, demoteHeadings(args.Content))
	if err == nil {
		err = store.IngestOne(srv.store, srv.docsPath, mdPath)
	}
	release()

	if err != nil {
		srv.logger.Error("write failed", "path", filePath, "err", err)
		srv.replyErr(enc, id, -32603, "write error: "+err.Error())
		return
	}
	srv.reply(enc, id, map[string]any{
		"content": []map[string]any{{
			"type": "text",
			"text": fmt.Sprintf("Saved to knowledge base: %s / %s", mdPath, args.Heading),
		}},
	})
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

// updateSection finds "## heading" in md (outside fenced code blocks) and
// replaces the whole section — its body and any ### subsections — with
// content. If the heading is absent, appends a new section at the end.
func updateSection(md, heading, content string) string {
	target := "## " + heading
	lines := strings.Split(md, "\n")
	start := -1
	inFence := false
	for i, line := range lines {
		if isFenceLine(line) {
			inFence = !inFence
			continue
		}
		if !inFence && strings.TrimRight(line, " ") == target {
			start = i
			break
		}
	}
	if start == -1 {
		return strings.TrimRight(md, "\n") + "\n\n" + target + "\n\n" + strings.TrimSpace(content) + "\n"
	}
	// The section ends at the next heading of level ≤ 2 outside a fence.
	// Deeper headings (### …) belong to this section and are replaced with it.
	end := len(lines)
	inFence = false
	for i := start + 1; i < len(lines); i++ {
		if isFenceLine(lines[i]) {
			inFence = !inFence
			continue
		}
		if !inFence {
			if lvl := headingLevel(lines[i]); lvl > 0 && lvl <= 2 {
				end = i
				break
			}
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

// demoteHeadings shifts top-level markdown headings in content down one level
// (# → ##, ## → ###) outside fenced code blocks, so the caller's
// "## <heading>" wrapper stays the sole section anchor and internal headings
// become searchable subsections instead of sibling sections. ### and deeper
// are left untouched — they already index as "Parent / Child" chunks.
func demoteHeadings(content string) string {
	lines := strings.Split(content, "\n")
	inFence := false
	for i, line := range lines {
		if isFenceLine(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if lvl := headingLevel(line); lvl == 1 || lvl == 2 {
			lines[i] = "#" + line
		}
	}
	return strings.Join(lines, "\n")
}

// isFenceLine reports whether line opens or closes a fenced code block.
func isFenceLine(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// headingLevel returns the heading level of a markdown line (# = 1, ## = 2, …),
// or 0 when the line is not a heading.
func headingLevel(line string) int {
	i := 0
	for i < len(line) && line[i] == '#' {
		i++
	}
	if i == 0 || i > 6 || i >= len(line) || line[i] != ' ' {
		return 0
	}
	return i
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
			if len(d.Tags) > 0 {
				fmt.Fprintf(&sb, "%s  [tags: %s]\n", d.Path, strings.Join(d.Tags, ", "))
			} else {
				fmt.Fprintf(&sb, "%s\n", d.Path)
			}
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
	if srv.docsPath == "" {
		srv.replyErr(enc, id, -32603, "server misconfigured: DOCS_PATH is not set — markdown files are the only source of truth")
		return
	}

	mdPath := args.Path
	if !strings.HasSuffix(mdPath, ".md") {
		mdPath += ".md"
	}
	filePath, err := safeFilePath(srv.docsPath, mdPath)
	if err != nil {
		srv.replyErr(enc, id, -32602, "invalid path: "+err.Error())
		return
	}

	release, err := lock.Acquire(srv.lockPath())
	if err != nil {
		srv.logger.Error("delete lock", "err", err)
		srv.replyErr(enc, id, -32603, "delete error: "+err.Error())
		return
	}

	// Index first, then file: if the DB row is missing but the .md file exists,
	// the file is still removed (a leftover would be re-ingested on next startup).
	n, dbErr := srv.store.DeleteDocument(mdPath)
	fileErr := os.Remove(filePath)
	release()

	fileExisted := fileErr == nil
	if fileErr != nil && !os.IsNotExist(fileErr) {
		// Hard error: if the file remains on disk, Ingest will resurrect it on next startup.
		srv.logger.Error("could not remove markdown file", "path", filePath, "err", fileErr)
		srv.replyErr(enc, id, -32603, "delete error: could not remove file from disk")
		return
	}
	if dbErr != nil && !fileExisted {
		srv.replyErr(enc, id, -32603, "not found: "+mdPath)
		return
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Deleted: %s", mdPath)
	switch {
	case dbErr == nil && fileExisted:
		fmt.Fprintf(&sb, " (%d sections removed, file deleted)", n)
	case dbErr == nil:
		fmt.Fprintf(&sb, " (%d sections removed, no .md file was on disk)", n)
	default:
		sb.WriteString(" (file deleted, no index entries existed)")
	}

	srv.reply(enc, id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": sb.String()}},
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

	// Exact lookup first: "drain node" → tools/drain-node.md. Deterministic and
	// immune to runbooks outranking the tool in search.
	if srv.docsPath != "" {
		slug := slugify(args.Name)
		if slug != "" {
			filePath, err := safeFilePath(srv.docsPath, "tools/"+slug+".md")
			if err == nil {
				if data, err := os.ReadFile(filePath); err == nil { //nolint:gosec // path validated by safeFilePath
					srv.reply(enc, id, map[string]any{
						"content": []map[string]any{{
							"type": "text",
							"text": formatToolFile(chunker.Split(string(data)), "tools/"+slug+".md"),
						}},
					})
					return
				}
			}
		}
	}

	// Fallback: prefix-scoped search over tools/ only.
	results, err := store.Search(srv.store, store.SearchOpts{
		Query:      args.Name,
		Limit:      10,
		PathPrefix: "tools/",
	})
	if err != nil {
		srv.replyErr(enc, id, -32603, "search error")
		return
	}

	if len(results) == 0 {
		srv.reply(enc, id, map[string]any{
			"content": []map[string]any{{
				"type": "text",
				"text": fmt.Sprintf("No tool found for %q. Store tools under tools/<name>.md with ### headings and ```code blocks``` — get_tool matches the name exactly (e.g. \"drain node\" finds tools/drain-node.md).", args.Name),
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

// formatToolFile renders a tool .md file (already split into chunks) as an
// MCP response: the first section containing a code block, with the code
// extracted for direct execution.
func formatToolFile(chunks []chunker.Chunk, path string) string {
	var sb strings.Builder
	for _, c := range chunks {
		code := extractCodeBlock(c.Content)
		if code == c.Content {
			continue // no fenced block in this section
		}
		fmt.Fprintf(&sb, "## %s\nSource: %s\n\n```\n%s\n```", c.Heading, path, code)
		return sb.String()
	}
	// No code block anywhere — return the full content as-is.
	if len(chunks) > 0 {
		fmt.Fprintf(&sb, "## %s\nSource: %s\n\n%s", chunks[0].Heading, path, chunks[0].Content)
	}
	return sb.String()
}

// slugify converts a tool name to a file slug: lowercase, runs of
// non-alphanumerics collapsed to single hyphens ("Drain Node!" → "drain-node").
func slugify(name string) string {
	var b strings.Builder
	lastHyphen := true // suppress a leading hyphen
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	return strings.TrimSuffix(b.String(), "-")
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

// scoreLabel describes match quality from the underlying evidence — keyword
// rank and raw cosine similarity — rather than the fused RRF score, which is
// tiny by construction and would label every vector-only hit "weak".
// neural selects cosine thresholds for real embeddings; TF-IDF similarities
// run lower, so its thresholds are more permissive.
func scoreLabel(r store.Result, neural bool) string {
	strongCos, relCos := 0.60, 0.35
	if !neural {
		strongCos, relCos = 0.45, 0.25
	}
	switch {
	case (r.FTSRank > 0 && r.FTSRank <= 3) || r.Cosine >= strongCos:
		return "strong match"
	case r.FTSRank > 0 || r.Cosine >= relCos:
		return "relevant"
	default:
		return "weak match"
	}
}

func formatResults(results []store.Result, query string, neural bool) string {
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
		if updated := formatDate(r.Updated); updated != "" {
			fmt.Fprintf(&sb, " [%s · updated %s]", scoreLabel(r, neural), updated)
		} else {
			fmt.Fprintf(&sb, " [%s]", scoreLabel(r, neural))
		}
		fmt.Fprintf(&sb, " ---\n")
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

// formatDate trims a SQLite datetime ("2026-08-02 14:33:05") to its date part.
func formatDate(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return ""
}
