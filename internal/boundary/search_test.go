package boundary

import (
	"strings"
	"testing"
)

// Search behaviour against the real IEBC data, written from the point of view
// of someone typing what they know.
func TestSearchFindsWhatKenyansType(t *testing.T) {
	tree := realTree(t)
	tests := []struct {
		query string
		level Level
		want  string // "Name, Constituency, County" of the first hit
	}{
		{"Ziwa la Ng'ombe", 0, "Ziwa la Ng'ombe, Nyali, Mombasa"},        // exact, with apostrophe
		{"ziwa la ngombe", LevelWard, "Ziwa la Ng'ombe, Nyali, Mombasa"}, // apostrophe dropped
		{"ziwa ngombe", LevelWard, "Ziwa la Ng'ombe, Nyali, Mombasa"},    // words skipped
		{"makadara", LevelWard, "Mji wa Kale/Makadara, Mvita, Mombasa"},  // second half of a slash name
		{"KIAMWANGI", 0, "Kiamwangi, Gatundu South, Kiambu"},             // upper case
		{"kiamwa", 0, "Kiamwangi, Gatundu South, Kiambu"},                // prefix while typing
		{"Kiamwngi", 0, "Kiamwangi, Gatundu South, Kiambu"},              // missing letter
		{"Kitisuru", 0, "Kitisuru, Westlands, Nairobi City"},
		{"Kitusuru", 0, "Kitisuru, Westlands, Nairobi City"},     // wrong vowel
		{"homabay", 0, "Homa Bay"},                               // no space
		{"muranga", 0, "Murang'a"},                               // no apostrophe
		{"nairobi", LevelCounty, "Nairobi City"},                 // common short name
		{"mount elgon", 0, "Mt. Elgon, Bungoma"},                 // alias
		{"mt elgon", 0, "Mt. Elgon, Bungoma"},                    // no dot
		{"mbita", 0, "Suba North, Homa Bay"},                     // former name
		{"elgeyo marakwet", 0, "Elgeyo/Marakwet"},                // slash typed as space
		{"south c", LevelWard, "South C, Langata, Nairobi City"}, // single-letter word
		{"manyatta b", 0, "Manyatta 'B', Kisumu East, Kisumu"},
		{"township kiharu", LevelWard, "Township, Kiharu, Murang'a"},  // disambiguate by constituency
		{"township bungoma", LevelWard, "Township, Kanduyi, Bungoma"}, // or by county
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := tree.Search(tt.query, SearchOptions{Level: tt.level, Limit: 5})
			if len(got) == 0 {
				t.Fatalf("no results")
			}
			if l := label(got[0]); l != tt.want {
				t.Errorf("first hit = %q, want %q; all: %v", l, tt.want, labels(got))
			}
		})
	}
}

func TestSearchRanksCountyBeforeSameNamedConstituencyAndWard(t *testing.T) {
	// "Kisumu" is a county, constituencies (Kisumu Central/East/West) and a ward.
	got := tree(t).Search("kisumu", SearchOptions{Limit: 10})
	if got[0].Unit.Level != LevelCounty {
		t.Errorf("first hit = %v, want the county; all: %v", got[0].Unit, labels(got))
	}
}

func TestSearchDuplicateWardNamesAllReturnedWithDistinctLabels(t *testing.T) {
	got := tree(t).Search("township", SearchOptions{Level: LevelWard, Limit: 50})
	if len(got) < 9 {
		t.Fatalf("got %d Township wards, want at least 9: %v", len(got), labels(got))
	}
	seen := map[string]bool{}
	for _, m := range got[:9] {
		if m.Unit.Name != "TOWNSHIP" {
			t.Errorf("exact matches should come first, got %q", m.Unit.Name)
		}
		l := label(m)
		if seen[l] {
			t.Errorf("duplicate label %q", l)
		}
		seen[l] = true
	}
}

func TestSearchLevelFilterAndLimit(t *testing.T) {
	tr := tree(t)
	for _, m := range tr.Search("central", SearchOptions{Level: LevelConstituency, Limit: 50}) {
		if m.Unit.Level != LevelConstituency {
			t.Errorf("level filter leaked %v", m.Unit)
		}
	}
	if n := len(tr.Search("ka", SearchOptions{Limit: 3})); n != 3 {
		t.Errorf("limit 3 returned %d", n)
	}
	if n := len(tr.Search("ka", SearchOptions{Limit: 500})); n != MaxSearchLimit {
		t.Errorf("limit is capped at %d, got %d", MaxSearchLimit, n)
	}
	if n := len(tr.Search("ka", SearchOptions{})); n != DefaultSearchLimit {
		t.Errorf("default limit = %d, want %d", n, DefaultSearchLimit)
	}
}

func TestSearchRejectsNoiseAndNeverReturnsNational(t *testing.T) {
	tr := tree(t)
	for _, q := range []string{"", " ", "a", "'", "--", "zzzzqqqq", "national"} {
		for _, m := range tr.Search(q, SearchOptions{Limit: 50}) {
			if q != "national" {
				t.Errorf("Search(%q) returned %v", q, m.Unit)
			}
			if m.Unit.Level == LevelNational {
				t.Errorf("Search(%q) returned the national unit", q)
			}
		}
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"ZIWA LA NG'OMBE":      "ziwa la ngombe",
		"MJI WA KALE/MAKADARA": "mji wa kale makadara",
		"Mt. Elgon":            "mt elgon",
		"  Heilu-Manyatta  ":   "heilu manyatta",
		"MANYATTA 'B'":         "manyatta b",
		"Murang’a":             "muranga",
		"Njabini\\Kiburu":      "njabini kiburu",
	} {
		if got := normalize(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEditDistance(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		max  int
		want int
	}{
		{"kitusuru", "kitisuru", 2, 1},
		{"kiamwngi", "kiamwangi", 2, 1},
		{"abcd", "abdc", 2, 1}, // transposition
		{"same", "same", 1, 0},
		{"short", "muchlongerword", 2, 3}, // capped at max+1
	} {
		if got := editDistance(tt.a, tt.b, tt.max); got != tt.want {
			t.Errorf("editDistance(%q,%q,%d) = %d, want %d", tt.a, tt.b, tt.max, got, tt.want)
		}
	}
}

func BenchmarkSearch(b *testing.B) {
	tr := realTree(b)
	queries := []string{"kiamwa", "Kitusuru", "ziwa ngombe", "township kiharu", "homabay"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tr.Search(queries[i%len(queries)], SearchOptions{Limit: 10})
	}
}

var cachedTree *Tree

func tree(t *testing.T) *Tree {
	if cachedTree == nil {
		cachedTree = realTree(t)
	}
	return cachedTree
}

func label(m Match) string {
	parts := []string{m.Unit.DisplayName}
	for _, a := range m.Ancestors {
		parts = append(parts, a.DisplayName)
	}
	return strings.Join(parts, ", ")
}

func labels(ms []Match) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = label(m)
	}
	return out
}
