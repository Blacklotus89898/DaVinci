package embed

import (
	"hash/fnv"
	"math"
	"regexp"
	"strings"
)

// DefaultDims is the TF-IDF vector dimension. 1024 halves hash-collision rate vs the old 512.
const DefaultDims = 1024

var (
	nonAlpha = regexp.MustCompile(`[^a-z0-9]+`)
	stops    = makeStops()

	// sreExpansions adds canonical synonyms for common SRE error terms so that a
	// query for "OOMKilled" also scores docs that say "oom" and vice-versa.
	sreExpansions = map[string][]string{
		"oomkilled":        {"oom"},
		"oom":              {"oomkilled"},
		"crashloopbackoff": {"crashloop"},
		"crashloop":        {"crashloopbackoff"},
		"diskpressure":     {"disk"},
		"imagepullbackoff": {"imagepull"},
		"errimagepull":     {"imagepull"},
		"imagepull":        {"imagepullbackoff"},
		"eviction":         {"evict"},
		"evict":            {"eviction"},
		"drain":            {"evict"},
		"deadline":         {"timeout"},
		"timeout":          {"deadline"},
		"unreachable":      {"notready"},
		"notready":         {"unreachable"},
	}
)

func makeStops() map[string]bool {
	words := strings.Fields(
		"the a an is are was were be been being have has had do does did will would could should " +
			"may might must shall to of in for on with as by at from this that these those it its " +
			"i we you he she they what which who how when where why and or not no nor so yet but if " +
			"then than because while although though until unless since after before about above " +
			"below between through during into onto upon within without",
	)
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// TokenizeRaw lowercases, strips non-alphanumeric, and removes stopwords and single-char
// tokens, but does NOT expand SRE synonyms. Use this for FTS5 AND-query building where
// synonym tokens injected into an AND expression break recall for compound words.
func TokenizeRaw(text string) []string {
	text = strings.ToLower(text)
	text = nonAlpha.ReplaceAllString(text, " ")
	words := strings.Fields(text)
	out := make([]string, 0, len(words))
	for _, w := range words {
		if len(w) <= 1 || stops[w] {
			continue
		}
		out = append(out, w)
	}
	return out
}

// Tokenize lowercases, strips non-alphanumeric, removes stopwords and single-char tokens,
// then expands SRE synonym pairs (once per unique source token per call).
func Tokenize(text string) []string {
	text = strings.ToLower(text)
	text = nonAlpha.ReplaceAllString(text, " ")
	words := strings.Fields(text)
	out := make([]string, 0, len(words))
	expanded := make(map[string]bool)
	for _, w := range words {
		if len(w) <= 1 || stops[w] {
			continue
		}
		out = append(out, w)
		if exps, ok := sreExpansions[w]; ok && !expanded[w] {
			expanded[w] = true
			out = append(out, exps...)
		}
	}
	return out
}

// TermFreq computes per-term frequency for a token slice.
func TermFreq(tokens []string) map[string]float64 {
	if len(tokens) == 0 {
		return nil
	}
	counts := make(map[string]int, len(tokens))
	for _, t := range tokens {
		counts[t]++
	}
	n := float64(len(tokens))
	tf := make(map[string]float64, len(counts))
	for t, c := range counts {
		tf[t] = float64(c) / n
	}
	return tf
}

// SmoothedIDF computes log((N+1)/(df+1))+1 (sklearn smooth IDF).
func SmoothedIDF(N, df int) float64 {
	return math.Log(float64(N+1)/float64(df+1)) + 1.0
}

// Vectorize produces a dims-dimensional TF-IDF vector using the hashing trick.
// Terms absent from idf use idfDefault. The returned slice is L2-normalised.
func Vectorize(tf map[string]float64, idf map[string]float64, idfDefault float64, dims int) []float32 {
	if dims <= 0 {
		return nil
	}
	vec := make([]float32, dims)
	for term, tfVal := range tf {
		idfVal, ok := idf[term]
		if !ok {
			idfVal = idfDefault
		}
		w := float32(tfVal * idfVal)
		h := hashTerm(term)
		dim := (h >> 1) % uint32(dims) //nolint:gosec // dims is always positive and bounded by embed.DefaultDims
		if h&1 == 1 {
			vec[dim] -= w
		} else {
			vec[dim] += w
		}
	}
	return Normalize(vec)
}

// CosineSim returns the cosine similarity of two unit vectors.
// Returns 0 if lengths differ or both are zero vectors.
func CosineSim(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dot float32
	for i := range a {
		dot += a[i] * b[i]
	}
	return dot
}

// Normalize scales vec to unit length in-place and returns it.
func Normalize(vec []float32) []float32 {
	var sum float32
	for _, v := range vec {
		sum += v * v
	}
	if sum == 0 {
		return vec
	}
	inv := float32(1.0 / math.Sqrt(float64(sum)))
	for i := range vec {
		vec[i] *= inv
	}
	return vec
}

// ToBytes serialises a vector as little-endian float32 bytes.
func ToBytes(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		bits := math.Float32bits(f)
		b[i*4] = byte(bits)
		b[i*4+1] = byte(bits >> 8)
		b[i*4+2] = byte(bits >> 16)
		b[i*4+3] = byte(bits >> 24)
	}
	return b
}

// FromBytes deserialises a vector from little-endian float32 bytes.
// Returns nil if b is empty or not a multiple of 4 bytes.
func FromBytes(b []byte) []float32 {
	if len(b) == 0 || len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		bits := uint32(b[i*4]) | uint32(b[i*4+1])<<8 | uint32(b[i*4+2])<<16 | uint32(b[i*4+3])<<24
		v[i] = math.Float32frombits(bits)
	}
	return v
}

func hashTerm(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}
