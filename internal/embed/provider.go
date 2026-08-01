package embed

// Provider is an optional external embedding backend (e.g. Ollama).
// When set on a Store it replaces TF-IDF hashing for chunk and query vectorisation.
type Provider interface {
	// Dims returns the vector dimension this provider produces.
	Dims() int
	// Embed returns a unit-length embedding vector for the given text.
	Embed(text string) ([]float32, error)
}
