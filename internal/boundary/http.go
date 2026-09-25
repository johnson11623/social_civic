package boundary

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/johnson11623/social_civic/internal/platform/httpjson"
	"github.com/johnson11623/social_civic/internal/platform/i18n"
	"github.com/johnson11623/social_civic/internal/platform/problem"
)

// Public endpoints: registration happens before login, so these need no auth.
// They expose only public administrative data.

// UnitRef is a unit in API responses.
type UnitRef struct {
	Level    Level  `json:"level"`
	Code     int    `json:"code"`
	IEBCCode string `json:"iebc_code"`
	Name     string `json:"name"`
}

func ref(u Unit) UnitRef {
	return UnitRef{Level: u.Level, Code: u.Code, IEBCCode: u.IEBCCode, Name: u.DisplayName}
}

// TreeNode is a unit with its children, for cascading County → Constituency → Ward pickers.
type TreeNode struct {
	UnitRef
	Children []TreeNode `json:"children,omitempty"`
}

// TreeResponse is the body of GET /v1/boundary/tree.
type TreeResponse struct {
	Version  string     `json:"version"`
	Counties []TreeNode `json:"counties"`
}

// SearchResult is one hit in GET /v1/boundary/search.
type SearchResult struct {
	UnitRef
	Label        string   `json:"label"` // e.g. "Township, Kiharu, Murang'a"
	Constituency *UnitRef `json:"constituency,omitempty"`
	County       *UnitRef `json:"county,omitempty"`
}

// SearchResponse is the body of GET /v1/boundary/search.
type SearchResponse struct {
	Query string         `json:"query"`
	Items []SearchResult `json:"items"`
}

// Handlers serves the boundary API from an in-memory tree.
type Handlers struct {
	Tree *Tree
	tree TreeResponse // built once; the tree is immutable
}

// NewHandlers precomputes the tree response.
func NewHandlers(t *Tree) *Handlers {
	h := &Handlers{Tree: t, tree: TreeResponse{Version: t.Version}}
	var build func(u Unit) TreeNode
	build = func(u Unit) TreeNode {
		n := TreeNode{UnitRef: ref(u)}
		for _, c := range t.Children(u.Level, u.Code) {
			n.Children = append(n.Children, build(c))
		}
		return n
	}
	for _, c := range t.Children(LevelNational, NationalCode) {
		h.tree.Counties = append(h.tree.Counties, build(c))
	}
	return h
}

// GetTree serves GET /v1/boundary/tree.
func (h *Handlers) GetTree(w http.ResponseWriter, r *http.Request) {
	etag := `"` + h.Tree.Version + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	httpjson.Write(w, http.StatusOK, h.tree)
}

// Search serves GET /v1/boundary/search?q=&level=&limit=.
func (h *Handlers) Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	q := strings.TrimSpace(query.Get("q"))
	if len(normalize(q)) < minQueryLen {
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgSearchTooShort,
			problem.FieldError{Field: "q", Code: "too_short"})
		return
	}
	if len(q) > 100 {
		problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgSearchTooLong,
			problem.FieldError{Field: "q", Code: "too_long"})
		return
	}
	opts := SearchOptions{}
	if l := query.Get("level"); l != "" {
		level, err := ParseLevel(l)
		if err != nil || level == LevelNational {
			problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgInvalidLevel,
				problem.FieldError{Field: "level", Code: "invalid"})
			return
		}
		opts.Level = level
	}
	if l := query.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 || n > MaxSearchLimit {
			problem.Write(w, r, http.StatusUnprocessableEntity, "validation_failed", i18n.MsgInvalidLimit,
				problem.FieldError{Field: "limit", Code: "out_of_range"})
			return
		}
		opts.Limit = n
	}

	resp := SearchResponse{Query: q, Items: []SearchResult{}}
	for _, m := range h.Tree.Search(q, opts) {
		item := SearchResult{UnitRef: ref(m.Unit)}
		label := []string{m.Unit.DisplayName}
		for _, a := range m.Ancestors {
			a := ref(a)
			label = append(label, a.Name)
			switch a.Level {
			case LevelConstituency:
				item.Constituency = &a
			case LevelCounty:
				item.County = &a
			}
		}
		item.Label = strings.Join(label, ", ")
		resp.Items = append(resp.Items, item)
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	httpjson.Write(w, http.StatusOK, resp)
}
