package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/blacklotus88888/knowledge-service/internal/embed"
	"github.com/blacklotus88888/knowledge-service/internal/store"
	"github.com/blacklotus88888/knowledge-service/mcp"
)

func main() {
	httpAddr := flag.String("http", "", "Start HTTP server on this address (e.g. :3737). Default: stdio MCP mode.")
	flag.Parse()

	dbPath := env("DB_PATH", "knowledge.db")
	docsPath := env("DOCS_PATH", "")
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

	srv := mcp.NewServer(db, logger, docsPath)

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

func runHTTP(srv *mcp.Server, db *store.Store, logger *slog.Logger, addr string) {
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

	// Health/readiness: includes a lightweight DB ping so load-balancer probes catch DB issues.
	// Excluded from auth so probes don't need a token.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if err := db.DB.QueryRowContext(r.Context(), `SELECT 1`).Scan(new(int)); err != nil {
			http.Error(w, "db: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		fmt.Fprintln(w, "ok")
	})

	var handler http.Handler = mux
	handler = withTimeout(30*time.Second, handler)
	if authToken != "" {
		handler = bearerAuth(authToken, handler)
		logger.Info("HTTP auth enabled")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpSrv := &http.Server{Addr: addr, Handler: handler}

	go func() {
		<-ctx.Done()
		logger.Info("shutting down HTTP server")
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutCtx) //nolint:errcheck
	}()

	logger.Info("knowledge-service ready (HTTP)", "addr", addr, "schema", addr+"/schema.json")
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("http error", "err", err)
		os.Exit(1)
	}
}

// withTimeout wraps a handler with a per-request context deadline.
func withTimeout(d time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
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
