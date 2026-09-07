package chunker

import (
	"bufio"
	"fmt"
	"strings"
)

// maxChunkChars is the soft upper bound on chunk content length.
// Chunks larger than this are split at paragraph boundaries.
const maxChunkChars = 2000

// Chunk is one indexable section of a markdown document.
type Chunk struct {
	Heading string
	Content string
}

// Split divides a markdown document into chunks.
//
// Splitting rules:
//   - Leading YAML frontmatter (--- ... ---) is stripped before parsing.
//   - Fenced code blocks (``` or ~~~) are never split — heading-like lines
//     inside a fence are treated as code.
//   - Each # heading starts a new chunk (document title level).
//   - Each ## heading starts a new chunk.
//   - Each ### heading starts a new chunk whose heading is prefixed with the
//     parent ## heading ("Parent / Child") to preserve context.
//   - Chunks whose content exceeds maxChunkChars are split at paragraph
//     boundaries with an index suffix on the heading ("Heading (2)", etc.).
func Split(markdown string) []Chunk {
	markdown = stripFrontmatter(markdown)

	var raw []Chunk
	var heading strings.Builder
	var content strings.Builder
	var parentH2 string // current ## heading context for ### chunks
	inFence := false

	flush := func() {
		h := strings.TrimSpace(heading.String())
		c := strings.TrimSpace(content.String())
		if h != "" || c != "" {
			raw = append(raw, Chunk{Heading: h, Content: c})
		}
		heading.Reset()
		content.Reset()
	}

	scanner := bufio.NewScanner(strings.NewReader(markdown))
	for scanner.Scan() {
		line := scanner.Text()
		if isFence(line) {
			inFence = !inFence
			content.WriteString(line)
			content.WriteByte('\n')
			continue
		}
		if inFence {
			content.WriteString(line)
			content.WriteByte('\n')
			continue
		}
		switch {
		case strings.HasPrefix(line, "### "):
			flush()
			sub := strings.TrimPrefix(line, "### ")
			if parentH2 != "" {
				heading.WriteString(parentH2 + " / " + sub)
			} else {
				heading.WriteString(sub)
			}
		case strings.HasPrefix(line, "## "):
			flush()
			h := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			parentH2 = h
			heading.WriteString(h)
		case strings.HasPrefix(line, "# "):
			flush()
			parentH2 = ""
			heading.WriteString(strings.TrimPrefix(line, "# "))
		default:
			content.WriteString(line)
			content.WriteByte('\n')
		}
	}
	flush()

	var chunks []Chunk
	for _, c := range raw {
		chunks = append(chunks, splitLarge(c)...)
	}
	return chunks
}

// stripFrontmatter removes a leading YAML frontmatter block (---\n...\n---\n).
func stripFrontmatter(s string) string {
	_, rest := frontmatterBlock(s)
	return rest
}

// frontmatterBlock splits s into its leading YAML frontmatter block (without
// the --- delimiters) and the remaining markdown. Both are empty/s when there
// is no frontmatter.
func frontmatterBlock(s string) (block, rest string) {
	s = strings.TrimLeft(s, " \t\r\n")
	if !strings.HasPrefix(s, "---") {
		return "", s
	}
	rest = s[3:]
	// Opening --- must be followed immediately by a newline, not e.g. "---title".
	if len(rest) == 0 || (rest[0] != '\n' && rest[0] != '\r') {
		return "", s
	}
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", s
	}
	block = rest[:idx]
	// Skip the optional newline after the closing ---
	rest = strings.TrimLeft(rest[idx+4:], "\r\n")
	return block, rest
}

// Tags parses a comma-separated tags value from leading YAML frontmatter.
// Returns nil when the document has no frontmatter or no tags key.
func Tags(markdown string) []string {
	block, _ := frontmatterBlock(markdown)
	if block == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(block, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found || strings.TrimSpace(key) != "tags" {
			continue
		}
		for _, tag := range strings.Split(value, ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				out = append(out, tag)
			}
		}
	}
	return out
}

// isFence reports whether line opens or closes a fenced code block.
func isFence(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// splitLarge splits a chunk whose content exceeds maxChunkChars at double-newline
// paragraph boundaries, adding an index suffix to the heading for each part.
func splitLarge(c Chunk) []Chunk {
	if len(c.Content) <= maxChunkChars {
		return []Chunk{c}
	}
	paragraphs := strings.Split(c.Content, "\n\n")
	var chunks []Chunk
	var buf strings.Builder
	part := 1

	for _, p := range paragraphs {
		if buf.Len() > 0 && buf.Len()+len(p) > maxChunkChars {
			chunks = append(chunks, Chunk{
				Heading: partHeading(c.Heading, part),
				Content: strings.TrimSpace(buf.String()),
			})
			buf.Reset()
			part++
		}
		if buf.Len() > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(p)
	}
	if buf.Len() > 0 {
		chunks = append(chunks, Chunk{
			Heading: partHeading(c.Heading, part),
			Content: strings.TrimSpace(buf.String()),
		})
	}
	return chunks
}

func partHeading(base string, part int) string {
	if base == "" || part == 1 {
		return base
	}
	return fmt.Sprintf("%s (%d)", base, part)
}
