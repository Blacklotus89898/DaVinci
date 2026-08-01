package embed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OllamaProvider calls a local Ollama instance for dense semantic embeddings.
// Recommended model: nomic-embed-text (768-dim, runs on CPU, excellent quality).
//
// Usage: set EMBED_PROVIDER=ollama, EMBED_URL=http://localhost:11434, EMBED_MODEL=nomic-embed-text
type OllamaProvider struct {
	url    string
	model  string
	dims   int
	client *http.Client
}

// NewOllamaProvider connects to an Ollama instance and probes the model to determine
// its output dimension. Returns an error if Ollama is unreachable or the model is missing.
// The probe uses a short 5s timeout so MCP startup is not blocked if Ollama is slow.
func NewOllamaProvider(url, model string) (*OllamaProvider, error) {
	p := &OllamaProvider{
		url:    strings.TrimRight(url, "/"),
		model:  model,
		client: &http.Client{Timeout: 60 * time.Second},
	}
	// Use a short-lived client for the probe so server startup is not blocked for 60s.
	probe := &http.Client{Timeout: 5 * time.Second}
	v, err := p.embedWith(probe, "probe")
	if err != nil {
		return nil, fmt.Errorf("ollama probe at %s with model %q failed — is Ollama running? (%w)", url, model, err)
	}
	p.dims = len(v)
	return p, nil
}

func (p *OllamaProvider) Dims() int { return p.dims }

// Embed sends text to Ollama and returns a normalised float32 embedding vector.
func (p *OllamaProvider) Embed(text string) ([]float32, error) {
	return p.embedWith(p.client, text)
}

func (p *OllamaProvider) embedWith(client *http.Client, text string) ([]float32, error) {
	body, err := json.Marshal(map[string]string{"model": p.model, "prompt": text})
	if err != nil {
		return nil, err
	}
	resp, err := client.Post(p.url+"/api/embeddings", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		return nil, fmt.Errorf("ollama returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Embedding []float64 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama decode: %w", err)
	}
	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("ollama returned empty embedding for model %q", p.model)
	}
	v := make([]float32, len(result.Embedding))
	for i, f := range result.Embedding {
		v[i] = float32(f)
	}
	return Normalize(v), nil
}
