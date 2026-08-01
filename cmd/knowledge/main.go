package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/blacklotus88888/knowledge-service/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	dbPath := getenv("DB_PATH", "knowledge.db")

	db, err := store.Open(dbPath)
	if err != nil {
		fatalf("open db: %v", err)
	}
	defer db.Close()

	switch os.Args[1] {
	case "ingest":
		doIngest(db)
	case "search":
		doSearch(db)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
	}
}

func doIngest(db *store.Store) {
	docsPath := getenv("DOCS_PATH", "docs")
	if len(os.Args) >= 3 {
		docsPath = os.Args[2]
	}
	fmt.Printf("Ingesting %s ...\n", docsPath)
	if err := store.Ingest(db, docsPath); err != nil {
		fatalf("ingest: %v", err)
	}
	fmt.Println("Done.")
}

func doSearch(db *store.Store) {
	if len(os.Args) < 3 {
		fatalf("usage: knowledge search <query> [limit]")
	}
	query := strings.Join(os.Args[2:], " ")
	limit := 5
	// If the last arg is a pure integer, treat it as the limit.
	if last := os.Args[len(os.Args)-1]; len(os.Args) > 3 {
		if n, err := strconv.Atoi(last); err == nil {
			limit = n
			query = strings.Join(os.Args[2:len(os.Args)-1], " ")
		}
	}

	results, err := store.Hybrid(db, query, limit, false)
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
	fmt.Fprintln(os.Stderr, "  ingest [path]    index .md files (default: $DOCS_PATH or docs/)")
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
