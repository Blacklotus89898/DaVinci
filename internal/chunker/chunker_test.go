package chunker

import (
	"strings"
	"testing"
)

func TestSplitEmpty(t *testing.T) {
	chunks := Split("")
	if len(chunks) != 0 {
		t.Fatalf("expected 0 chunks for empty input, got %d", len(chunks))
	}
}

func TestSplitTitleOnly(t *testing.T) {
	md := "# My Title\nSome body text.\n"
	chunks := Split(md)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Heading != "My Title" {
		t.Errorf("heading = %q, want %q", chunks[0].Heading, "My Title")
	}
	if chunks[0].Content != "Some body text." {
		t.Errorf("content = %q, want %q", chunks[0].Content, "Some body text.")
	}
}

func TestSplitSections(t *testing.T) {
	md := `# Doc Title
Intro paragraph.

## Section One
Content of section one.

## Section Two
Content of section two.
`
	chunks := Split(md)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Heading != "Doc Title" {
		t.Errorf("[0] heading = %q", chunks[0].Heading)
	}
	if chunks[1].Heading != "Section One" {
		t.Errorf("[1] heading = %q", chunks[1].Heading)
	}
	if chunks[2].Heading != "Section Two" {
		t.Errorf("[2] heading = %q", chunks[2].Heading)
	}
}

func TestSplitNoHeadings(t *testing.T) {
	md := "Just some text without any headings.\nSecond line.\n"
	chunks := Split(md)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for headingless text, got %d", len(chunks))
	}
	if chunks[0].Heading != "" {
		t.Errorf("expected empty heading, got %q", chunks[0].Heading)
	}
}

func TestSplitConsecutiveHeadings(t *testing.T) {
	md := "## A\n## B\nContent B.\n"
	chunks := Split(md)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Heading != "A" || chunks[0].Content != "" {
		t.Errorf("[0] unexpected: %+v", chunks[0])
	}
	if chunks[1].Heading != "B" {
		t.Errorf("[1] heading = %q", chunks[1].Heading)
	}
}

func TestSplitSubsections(t *testing.T) {
	md := `# Doc
Intro.

## Section
Section body.

### Sub A
Sub A content.

### Sub B
Sub B content.
`
	chunks := Split(md)
	// Doc, Section, Section / Sub A, Section / Sub B
	if len(chunks) != 4 {
		t.Fatalf("expected 4 chunks, got %d: %+v", len(chunks), chunks)
	}
	if chunks[2].Heading != "Section / Sub A" {
		t.Errorf("### heading = %q, want %q", chunks[2].Heading, "Section / Sub A")
	}
	if chunks[3].Heading != "Section / Sub B" {
		t.Errorf("### heading = %q, want %q", chunks[3].Heading, "Section / Sub B")
	}
	if !strings.Contains(chunks[2].Content, "Sub A content") {
		t.Errorf("subsection content missing: %q", chunks[2].Content)
	}
}

func TestSplitFrontmatter(t *testing.T) {
	md := "---\ntitle: My Doc\ndate: 2024-01-01\n---\n# Real Title\nActual content.\n"
	chunks := Split(md)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk after stripping frontmatter, got %d: %+v", len(chunks), chunks)
	}
	if chunks[0].Heading != "Real Title" {
		t.Errorf("heading = %q, want %q", chunks[0].Heading, "Real Title")
	}
	if strings.Contains(chunks[0].Content, "title:") {
		t.Error("frontmatter leaked into chunk content")
	}
}

func TestSplitNoFrontmatter(t *testing.T) {
	// A doc that starts with --- in content (not frontmatter) must not be stripped.
	md := "# Doc\n---\nThis is a horizontal rule.\n"
	chunks := Split(md)
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}
}

func TestSplitIgnoresHeadingsInCodeFences(t *testing.T) {
	md := "# Runbook\n\n## Fix\nApply this manifest:\n\n```yaml\n## this is a YAML comment, not a heading\napp: argocd\n```\n\nReal content after the block.\n"
	chunks := Split(md)

	// The fenced "## this is a YAML comment" must NOT start a new chunk:
	// expect exactly Doc + Fix.
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks (fence respected), got %d: %+v", len(chunks), chunks)
	}
	if chunks[1].Heading != "Fix" {
		t.Errorf("[1] heading = %q, want %q", chunks[1].Heading, "Fix")
	}
	if !strings.Contains(chunks[1].Content, "## this is a YAML comment") {
		t.Errorf("fenced heading line should stay inside the Fix chunk content: %q", chunks[1].Content)
	}
	if !strings.Contains(chunks[1].Content, "Real content after the block") {
		t.Errorf("content after the fence should stay in the Fix chunk: %q", chunks[1].Content)
	}
}

func TestSplitTildeFence(t *testing.T) {
	md := "## Section\n~~~\n## not a heading\n~~~\n"
	chunks := Split(md)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d: %+v", len(chunks), chunks)
	}
	if !strings.Contains(chunks[0].Content, "## not a heading") {
		t.Errorf("tilde-fenced heading leaked into a new chunk: %+v", chunks)
	}
}

func TestTags(t *testing.T) {
	cases := []struct {
		name string
		md   string
		want []string
	}{
		{"simple", "---\ntitle: X\ntags: argocd, oom, k8s\n---\n# T\nBody\n", []string{"argocd", "oom", "k8s"}},
		{"no tags key", "---\ntitle: X\n---\n# T\n", nil},
		{"no frontmatter", "# T\ntags: ignored\n", nil},
		{"empty tags", "---\ntags:\n---\n# T\n", nil},
		{"messy separators", "---\ntags:  postgres ,,, replication \n---\n# T\n", []string{"postgres", "replication"}},
	}
	for _, c := range cases {
		got := Tags(c.md)
		if len(got) != len(c.want) {
			t.Errorf("%s: Tags = %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: Tags[%d] = %q, want %q", c.name, i, got[i], c.want[i])
			}
		}
	}
}

func TestSplitLargeChunkIsSplit(t *testing.T) {
	// Build a chunk exceeding maxChunkChars (2000 chars) with paragraph breaks.
	para := strings.Repeat("word ", 100) // ~500 chars
	md := "## Big Section\n\n" + para + "\n\n" + para + "\n\n" + para + "\n\n" + para + "\n\n" + para + "\n"
	chunks := Split(md)
	if len(chunks) < 2 {
		t.Fatalf("large section should be split into ≥2 chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		if len(c.Content) > maxChunkChars+500 {
			t.Errorf("chunk content length %d exceeds cap", len(c.Content))
		}
	}
	// Second chunk heading should have index suffix.
	if !strings.Contains(chunks[1].Heading, "(2)") {
		t.Errorf("expected (2) suffix in split heading, got %q", chunks[1].Heading)
	}
}
