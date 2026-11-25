package chunker

import (
	"reflect"
	"testing"
)

func TestFreshnessOf(t *testing.T) {
	cases := map[string]Freshness{
		"---\ntitle: A\ncovers:\n  - src/**\n  - \"lib/*.ts\"\nlast_verified: 2026-11-14\n---\n# A\n": {Covers: []string{"src/**", "lib/*.ts"}, LastVerified: "2026-11-14"},
		"---\r\ncovers: [a/**, b]\r\nlast_verified: '2026-01-02'\r\n---\r\n# B\r\n":                   {Covers: []string{"a/**", "b"}, LastVerified: "2026-01-02"},
		"---\ncovers:\ntags: x\n- not-a-cover\n---\n":                                                 {},
		"# no frontmatter\n- item\n":                                                                  {},
	}
	for in, want := range cases {
		if got := FreshnessOf(in); !reflect.DeepEqual(got, want) {
			t.Errorf("FreshnessOf(%q) = %+v, want %+v", in, got, want)
		}
	}
}
