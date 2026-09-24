package boundary

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func get(t *testing.T, h http.HandlerFunc, target string, header ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestGetTree(t *testing.T) {
	h := NewHandlers(tree(t))
	rec := get(t, h.GetTree, "/v1/boundary/tree")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp TreeResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Version != IEBC2022 || len(resp.Counties) != 47 {
		t.Fatalf("version=%s counties=%d", resp.Version, len(resp.Counties))
	}
	constituencies, wards := 0, 0
	for _, c := range resp.Counties {
		if c.Level != LevelCounty {
			t.Errorf("county node has level %v", c.Level)
		}
		constituencies += len(c.Children)
		for _, k := range c.Children {
			wards += len(k.Children)
		}
	}
	if constituencies != 290 || wards != 1450 {
		t.Errorf("tree has %d constituencies and %d wards", constituencies, wards)
	}

	etag := rec.Header().Get("ETag")
	if etag == "" || rec.Header().Get("Cache-Control") == "" {
		t.Error("tree must be cacheable (ETag + Cache-Control)")
	}
	if rec := get(t, h.GetTree, "/v1/boundary/tree", "If-None-Match", etag); rec.Code != http.StatusNotModified {
		t.Errorf("conditional GET status = %d, want 304", rec.Code)
	}
}

func TestSearchEndpoint(t *testing.T) {
	h := NewHandlers(tree(t))
	rec := get(t, h.Search, "/v1/boundary/search?level=ward&limit=3&q="+url.QueryEscape("ziwa ngombe"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var resp SearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) == 0 {
		t.Fatal("no items")
	}
	first := resp.Items[0]
	if first.Code != 17 || first.IEBCCode != "0017" || first.Level != LevelWard ||
		first.Label != "Ziwa la Ng'ombe, Nyali, Mombasa" ||
		first.Constituency == nil || first.Constituency.Code != 4 ||
		first.County == nil || first.County.Code != 1 {
		t.Errorf("first item = %+v", first)
	}
}

func TestSearchEndpointEmptyResultIsEmptyArray(t *testing.T) {
	rec := get(t, NewHandlers(tree(t)).Search, "/v1/boundary/search?q=zzzzqqqq")
	if rec.Code != http.StatusOK || !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("status = %d", rec.Code)
	}
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	if string(raw["items"]) != "[]" {
		t.Errorf("items = %s, want []", raw["items"])
	}
}

func TestSearchEndpointValidation(t *testing.T) {
	h := NewHandlers(tree(t))
	for name, target := range map[string]string{
		"missing q":      "/v1/boundary/search",
		"one letter":     "/v1/boundary/search?q=a",
		"punctuation":    "/v1/boundary/search?q=''",
		"too long":       "/v1/boundary/search?q=" + strings.Repeat("a", 101),
		"bad level":      "/v1/boundary/search?q=ka&level=village",
		"national level": "/v1/boundary/search?q=ka&level=national",
		"limit zero":     "/v1/boundary/search?q=ka&limit=0",
		"limit too big":  "/v1/boundary/search?q=ka&limit=51",
		"limit not int":  "/v1/boundary/search?q=ka&limit=ten",
	} {
		t.Run(name, func(t *testing.T) {
			if rec := get(t, h.Search, target); rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("status = %d, want 422", rec.Code)
			}
		})
	}
}
