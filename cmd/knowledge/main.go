package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/blacklotus88888/knowledge-service/internal/config"
	"github.com/blacklotus88888/knowledge-service/internal/embed"
	"github.com/blacklotus88888/knowledge-service/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	// Same resolution as the server: env > <install>/knowledge.env > install defaults.
	root, _, err := config.ApplyForExecutable()
	if err != nil {
		fatalf("settings: %v", err)
	}
	dbPath := getenv("DB_PATH", "knowledge.db")

	db, err := store.Open(dbPath)
	if err != nil {
		fatalf("open db: %v", err)
	}
	defer db.Close() //nolint:errcheck

	applyEmbedConfig(db)

	switch os.Args[1] {
	case "ingest":
		doIngest(db, root)
	case "gaps":
		doGaps(db)
	case "search":
		doSearch(db)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
	}
}

// applyEmbedConfig mirrors the provider/dims setup from the server binary.
func applyEmbedConfig(db *store.Store) {
	if dimsStr := getenv("EMBED_DIMS", ""); dimsStr != "" {
		if n, err := strconv.Atoi(dimsStr); err == nil && n > 0 {
			db.SetTFIDFDims(n)
		}
	}
	if strings.ToLower(getenv("EMBED_PROVIDER", "")) == "ollama" {
		url := getenv("EMBED_URL", "http://localhost:11434")
		model := getenv("EMBED_MODEL", "nomic-embed-text")
		p, err := embed.NewOllamaProvider(url, model)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: ollama unavailable (%v), falling back to TF-IDF\n", err)
			return
		}
		db.SetProvider(p)
		fmt.Printf("embedding: ollama/%s (%d dims)\n", model, p.Dims())
	}
}

func doIngest(db *store.Store, root string) {
	docsPath := getenv("DOCS_PATH", "docs")
	if len(os.Args) >= 3 {
		docsPath = os.Args[2]
	}
	// Mounts must be ingested too: Ingest prunes every doc it did not walk.
	mounts, err := config.Mounts(os.Getenv("KB_MOUNTS"), root, docsPath)
	if err != nil {
		fatalf("%v", err)
	}
	fmt.Printf("Ingesting %s ...\n", docsPath)
	for _, m := range mounts {
		fmt.Printf("  + %s/ (read-only) from %s\n", m.Name, m.Root)
	}
	if err := store.Ingest(db, docsPath, mounts...); err != nil {
		fatalf("ingest: %v", err)
	}
	fmt.Println("Done.")
}

// doGaps lists searches that found nothing useful — the knowledge to write next.
func doGaps(db *store.Store) {
	gaps, err := db.Gaps(30)
	if err != nil {
		fatalf("gaps: %v", err)
	}
	if len(gaps) == 0 {
		fmt.Println("No unanswered searches logged.")
		return
	}
	fmt.Printf("%5s  %-5s  %-10s  %s\n", "times", "best", "last", "query")
	for _, g := range gaps {
		fmt.Printf("%5d  %-5s  %-10.10s  %s\n", g.Count, g.Outcome, g.Last, g.Query)
	}
}

func doSearch(db *store.Store) {
	if len(os.Args) < 3 {
		fatalf("usage: knowledge search <query> [limit]")
	}
	query := strings.Join(os.Args[2:], " ")
	limit := 5
	if last := os.Args[len(os.Args)-1]; len(os.Args) > 3 {
		if n, err := strconv.Atoi(last); err == nil {
			limit = n
			query = strings.Join(os.Args[2:len(os.Args)-1], " ")
		}
	}

	results, err := store.Search(db, store.SearchOpts{Query: query, Limit: limit})
	if err != nil {
		fatalf("search: %v", err)
	}
	if len(results) == 0 {
		fmt.Printf("No results for %q\n", query)
		return
	}
	for i, r := range results {
		fmt.Printf("--- [%d] %s", i+1, r.Path)
		if r.Heading != "" {
			fmt.Printf(" / %s", r.Heading)
		}
		fmt.Printf(" (score %.4f) ---\n%s\n\n", r.Score, r.Content)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: knowledge <command> [args]")
	fmt.Fprintln(os.Stderr, "  ingest [path]    index .md files (default: $DOCS_PATH or docs/) plus KB_MOUNTS")
	fmt.Fprintln(os.Stderr, "  gaps             most-asked searches that found nothing useful")
	fmt.Fprintln(os.Stderr, "  search <query>   search the knowledge base")
	os.Exit(1)
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
