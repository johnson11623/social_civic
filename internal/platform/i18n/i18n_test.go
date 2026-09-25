package i18n

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestParseAcceptLanguage(t *testing.T) {
	for header, want := range map[string]Lang{
		"":                          Swahili, // default
		"sw":                        Swahili,
		"sw-KE":                     Swahili,
		"en":                        English,
		"en-KE":                     English,
		"en-GB,en;q=0.9":            English,
		"fr-FR,en;q=0.8,sw;q=0.5":   English, // best supported preference
		"fr-FR,sw;q=0.9,en;q=0.8":   Swahili,
		"fr":                        Swahili, // unsupported → default
		"*":                         Swahili,
		"not a language;;;q=banana": Swahili, // garbage → default
	} {
		if got := Parse(header); got != want {
			t.Errorf("Parse(%q) = %s, want %s", header, got, want)
		}
	}
}

func TestFromRequest(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Language", "en-US")
	if got := FromRequest(r); got != English {
		t.Errorf("FromRequest = %s", got)
	}
}

func TestCatalogsHaveTheSameKeys(t *testing.T) {
	if missing := missingKeys(catalogs); len(missing) > 0 {
		t.Fatalf("catalog keys missing or empty: %v", missing)
	}
}

// Every Msg* constant must exist in every catalog, and every catalog key
// must be declared (so unused translations don't accumulate).
func TestKeyConstantsMatchCatalogs(t *testing.T) {
	declared := declaredKeys(t)
	for _, l := range Supported {
		for k := range declared {
			if _, ok := catalogs[l][k]; !ok {
				t.Errorf("%s catalog is missing %s", l, k)
			}
		}
		for k := range catalogs[l] {
			if !declared[k] && !strings.HasPrefix(string(k), "status.") {
				t.Errorf("%s catalog has undeclared key %s", l, k)
			}
		}
	}
}

func TestTranslationsDiffer(t *testing.T) {
	// Guards against a key copied into sw.json untranslated.
	for k, en := range catalogs[English] {
		if sw := catalogs[Swahili][k]; sw == en {
			t.Errorf("%s is identical in en and sw: %q", k, en)
		}
	}
}

func TestTFallsBack(t *testing.T) {
	if got := T(English, MsgInvalidNationalID); got != "National ID must be 8 digits." {
		t.Errorf("en = %q", got)
	}
	if got := T(Swahili, MsgInvalidNationalID); !strings.Contains(got, "tarakimu 8") {
		t.Errorf("sw = %q", got)
	}
	if got := T("fr", MsgInvalidNationalID); got != T(Default, MsgInvalidNationalID) {
		t.Errorf("unsupported language should fall back to default, got %q", got)
	}
	if got := T(English, "no.such.key"); got != "no.such.key" {
		t.Errorf("unknown key should return itself, got %q", got)
	}
	if got := StatusTitle(Swahili, 409); got != "Mgongano" {
		t.Errorf("sw 409 title = %q", got)
	}
	if got := StatusTitle(Swahili, 418); got != "I'm a teapot" {
		t.Errorf("uncatalogued status should use net/http text, got %q", got)
	}
}

func declaredKeys(t *testing.T) map[Key]bool {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "i18n.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[Key]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || !strings.HasPrefix(vs.Names[0].Name, "Msg") || len(vs.Values) != 1 {
			return true
		}
		if lit, ok := vs.Values[0].(*ast.BasicLit); ok {
			v, _ := strconv.Unquote(lit.Value)
			keys[Key(v)] = true
		}
		return true
	})
	if len(keys) == 0 {
		t.Fatal("found no Msg* constants")
	}
	return keys
}
