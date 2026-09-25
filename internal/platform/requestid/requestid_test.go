package requestid

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

func TestMiddleware(t *testing.T) {
	var seen string
	h := Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = middleware.GetReqID(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(Header, "forged\nlog line")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	got := rec.Header().Get(Header)
	if !strings.HasPrefix(got, "req_") || len(got) != len("req_")+36 {
		t.Errorf("X-Request-ID = %q", got)
	}
	if seen != got {
		t.Errorf("context id %q != header id %q", seen, got)
	}
}
