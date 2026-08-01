package embed

import (
	"math"
	"testing"
)

func TestTokenize(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"Hello World", []string{"hello", "world"}},
		{"the quick brown fox", []string{"quick", "brown", "fox"}}, // "the" is a stop word
		// OOM expands to ["oom","oomkilled"]; Kubernetes and Pod have no expansions.
		{"Kubernetes Pod OOM", []string{"kubernetes", "pod", "oom", "oomkilled"}},
		{"", nil},
		{"a", nil}, // single-char token stripped
	}
	for _, c := range cases {
		got := Tokenize(c.in)
		if len(got) != len(c.want) {
			t.Errorf("Tokenize(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("Tokenize(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestTokenizeSynonymExpansion(t *testing.T) {
	// oomkilled should expand to include "oom", and vice-versa.
	got := Tokenize("OOMKilled")
	found := map[string]bool{}
	for _, t := range got {
		found[t] = true
	}
	if !found["oomkilled"] || !found["oom"] {
		t.Errorf("OOMKilled should expand to both oomkilled and oom; got %v", got)
	}

	// Each source token should only trigger expansion once even if repeated.
	got2 := Tokenize("OOMKilled OOMKilled")
	oomCount := 0
	for _, tok := range got2 {
		if tok == "oom" {
			oomCount++
		}
	}
	if oomCount != 1 {
		t.Errorf("oom expansion should appear exactly once per unique source; got %d times", oomCount)
	}
}

func TestTermFreq(t *testing.T) {
	tokens := []string{"go", "go", "fast"}
	tf := TermFreq(tokens)
	if len(tf) != 2 {
		t.Fatalf("expected 2 unique terms, got %d", len(tf))
	}
	if math.Abs(tf["go"]-2.0/3.0) > 1e-9 {
		t.Errorf("tf[go] = %v, want %v", tf["go"], 2.0/3.0)
	}
	if math.Abs(tf["fast"]-1.0/3.0) > 1e-9 {
		t.Errorf("tf[fast] = %v, want %v", tf["fast"], 1.0/3.0)
	}
}

func TestTermFreqEmpty(t *testing.T) {
	if tf := TermFreq(nil); tf != nil {
		t.Errorf("expected nil for empty input, got %v", tf)
	}
}

func TestVectorizeIsUnitLength(t *testing.T) {
	tokens := Tokenize("kubernetes pod deployment restart oom")
	tf := TermFreq(tokens)
	idf := map[string]float64{"kubernetes": 2.0, "pod": 1.5, "deployment": 1.8, "restart": 2.1, "oom": 2.5, "oomkilled": 2.4}
	vec := Vectorize(tf, idf, 1.0, DefaultDims)

	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if math.Abs(norm-1.0) > 1e-5 {
		t.Errorf("vector norm = %v, want 1.0", norm)
	}
}

func TestCosineSimIdentical(t *testing.T) {
	tf := TermFreq(Tokenize("argocd sync failed"))
	idf := map[string]float64{"argocd": 2.0, "sync": 1.5, "failed": 1.8}
	vec := Vectorize(tf, idf, 1.0, DefaultDims)
	sim := CosineSim(vec, vec)
	if math.Abs(float64(sim)-1.0) > 1e-5 {
		t.Errorf("CosineSim(v, v) = %v, want 1.0", sim)
	}
}

func TestCosineSimZeroVector(t *testing.T) {
	a := make([]float32, DefaultDims)
	b := make([]float32, DefaultDims)
	a[0] = 1.0
	// b is zero vector → dot = 0
	sim := CosineSim(a, b)
	if sim != 0 {
		t.Errorf("CosineSim with zero vector = %v, want 0", sim)
	}
}

func TestCosineSimDifferentLengths(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{1, 0}
	if CosineSim(a, b) != 0 {
		t.Error("CosineSim of different-length vectors should be 0")
	}
}

func TestBytesRoundtrip(t *testing.T) {
	tf := TermFreq(Tokenize("sqlite fts5 bm25 hybrid search"))
	idf := map[string]float64{"sqlite": 2.0, "fts5": 3.0, "bm25": 2.5, "hybrid": 1.8, "search": 1.2}
	original := Vectorize(tf, idf, 1.0, DefaultDims)

	b := ToBytes(original)
	if len(b) != DefaultDims*4 {
		t.Fatalf("ToBytes len = %d, want %d", len(b), DefaultDims*4)
	}
	recovered := FromBytes(b)
	if recovered == nil {
		t.Fatal("FromBytes returned nil")
	}
	for i := range original {
		if original[i] != recovered[i] {
			t.Errorf("dim %d: original %v != recovered %v", i, original[i], recovered[i])
		}
	}
}

func TestFromBytesWrongLen(t *testing.T) {
	if v := FromBytes([]byte{1, 2, 3}); v != nil {
		t.Error("expected nil for wrong-length input")
	}
}

func TestFromBytesEmpty(t *testing.T) {
	if v := FromBytes(nil); v != nil {
		t.Error("expected nil for nil input")
	}
}

func TestSmoothedIDF(t *testing.T) {
	idf := SmoothedIDF(10, 10)
	if idf <= 0 {
		t.Errorf("SmoothedIDF(10,10) = %v, expected > 0", idf)
	}
	rare := SmoothedIDF(100, 1)
	common := SmoothedIDF(100, 90)
	if rare <= common {
		t.Errorf("rare IDF %v should be > common IDF %v", rare, common)
	}
}

// --- Benchmarks ---

func BenchmarkVectorize(b *testing.B) {
	tokens := Tokenize("kubernetes argocd deployment pod sync failed oom disk pressure cilium")
	tf := TermFreq(tokens)
	idf := map[string]float64{
		"kubernetes": 2.1, "argocd": 2.4, "deployment": 1.9,
		"pod": 1.5, "sync": 1.8, "failed": 2.0, "oom": 2.5, "disk": 2.2,
		"pressure": 2.3, "cilium": 2.6,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Vectorize(tf, idf, 1.0, DefaultDims)
	}
}

func BenchmarkCosineSim(b *testing.B) {
	tf := TermFreq(Tokenize("hybrid search sqlite fts5"))
	idf := map[string]float64{"hybrid": 2.0, "search": 1.5, "sqlite": 2.1, "fts5": 2.8}
	a := Vectorize(tf, idf, 1.0, DefaultDims)
	tf2 := TermFreq(Tokenize("bm25 full text search ranking"))
	vec2 := Vectorize(tf2, idf, 1.0, DefaultDims)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CosineSim(a, vec2)
	}
}
