@AGENTS.md

## Claude Code only

- Registered user-scope as the `knowledge` MCP, running `bin/knowledge-service(.exe)` — after code
  changes, `make build` and restart Claude Code sessions so the server picks them up.
- Ollama at http://localhost:11434; without it, search silently degrades to TF-IDF.
