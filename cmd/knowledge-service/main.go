package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/blacklotus88888/knowledge-service/internal/embed"
	"github.com/blacklotus88888/knowledge-service/internal/store"
	"github.com/blacklotus88888/knowledge-service/mcp"
)

func main() {
	httpAddr := flag.String("http", "", "Start HTTP server on this address (e.g. :3737). Default: stdio MCP mode.")
	flag.Parse()

	dbPath := env("DB_PATH", "knowledge.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel(env("LOG_LEVEL", "info")),
	}))

	db, err := store.Open(dbPath)
	if err != nil {
		logger.Error("failed to open database", "path", dbPath, "err", err)
		os.Exit(1)
	}
	defer db.Close()

	// Optional: override TF-IDF dimension (only matters when EMBED_PROVIDER != ollama).
	if dimsStr := env("EMBED_DIMS", ""); dimsStr != "" {
		if n, err := strconv.Atoi(dimsStr); err == nil && n > 0 {
			db.SetTFIDFDims(n)
		}
	}

	// Optional: use Ollama for dense semantic embeddings.
	// Set EMBED_PROVIDER=ollama to enable; EMBED_URL and EMBED_MODEL to configure.
	if strings.ToLower(env("EMBED_PROVIDER", "")) == "ollama" {
		ollamaURL := env("EMBED_URL", "http://localhost:11434")
		ollamaModel := env("EMBED_MODEL", "nomic-embed-text")
		p, err := embed.NewOllamaProvider(ollamaURL, ollamaModel)
		if err != nil {
			logger.Warn("Ollama unavailable, falling back to TF-IDF", "err", err)
		} else {
			db.SetProvider(p)
			logger.Info("embedding provider", "type", "ollama", "model", ollamaModel, "dims", p.Dims())
		}
	}

	srv := mcp.NewServer(db, logger)

	if *httpAddr != "" {
		// HTTP mode benefits from concurrent readers; WAL supports multiple connections.
		db.SetMaxConns(4)
		runHTTP(srv, db, logger, *httpAddr)
		return
	}

	logger.Info("knowledge-service ready (stdio)", "db", dbPath)
	if err := srv.Run(os.Stdin, os.Stdout); err != nil {
		logger.Error("server error", "err", err)
		os.Exit(1)
	}
}

func runHTTP(srv *mcp.Server, _ *store.Store, logger *slog.Logger, addr string) {
	authToken := env("AUTH_TOKEN", "")

	mux := http.NewServeMux()

	// MCP-compatible: accepts any JSON-RPC message, returns JSON-RPC response.
	mux.HandleFunc("POST /mcp", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			http.Error(w, "read error", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		srv.HandleRequest(body, w)
	})

	// OpenAI function-calling schema — paste into GPT-4o / Gemini / Ollama prompts.
	mux.HandleFunc("GET /schema.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tools":      mcp.OpenAITools(),
			"mcp_server": fmt.Sprintf("http://%s/mcp", addr),
		})
	})

	// Health / readiness — excluded from auth so load balancers can probe.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	var handler http.Handler = mux
	if authToken != "" {
		handler = bearerAuth(authToken, mux)
		logger.Info("HTTP auth enabled")
	}

	logger.Info("knowledge-service ready (HTTP)", "addr", addr, "schema", addr+"/schema.json")
	if err := http.ListenAndServe(addr, handler); err != nil {
		logger.Error("http error", "err", err)
		os.Exit(1)
	}
}

// bearerAuth wraps a handler requiring "Authorization: Bearer <token>" on all
// non-health paths. The token is read from the AUTH_TOKEN environment variable.
func bearerAuth(token string, next http.Handler) http.Handler {
	want := "Bearer " + token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Authorization") != want {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func logLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
