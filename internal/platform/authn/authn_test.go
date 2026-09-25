package authn

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeVerifier map[string]error

func (f fakeVerifier) VerifyAccess(tok string) (Principal, error) {
	if err, ok := f[tok]; ok {
		return Principal{}, err
	}
	return Principal{Subject: "user-" + tok, Ward: 17}, nil
}

func TestMiddleware(t *testing.T) {
	v := fakeVerifier{"old": ErrExpired, "bad": ErrInvalid}
	var got Principal
	var rejected string
	h := Middleware(v, func(w http.ResponseWriter, _ *http.Request, code string) {
		rejected = code
		w.WriteHeader(http.StatusUnauthorized)
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = FromContext(r.Context())
	}))

	for header, want := range map[string]string{
		"":             "unauthenticated",
		"Bearer":       "unauthenticated",
		"Bearer   ":    "unauthenticated",
		"Basic abc":    "unauthenticated",
		"Bearer old":   "token_expired",
		"Bearer bad":   "token_invalid",
		"Bearer good":  "",
		"bearer good":  "", // scheme is case-insensitive
		"BEARER  good": "",
	} {
		rejected, got = "", Principal{}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		h.ServeHTTP(httptest.NewRecorder(), req)
		if rejected != want {
			t.Errorf("%q: rejected = %q, want %q", header, rejected, want)
		}
		if want == "" && got.Subject != "user-good" {
			t.Errorf("%q: principal = %+v", header, got)
		}
	}
}

func TestFromContextOutsideMiddleware(t *testing.T) {
	if _, ok := FromContext(httptest.NewRequest(http.MethodGet, "/", nil).Context()); ok {
		t.Error("expected no principal")
	}
}
