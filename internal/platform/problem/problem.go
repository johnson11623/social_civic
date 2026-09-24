// Package problem writes RFC 7807 Problem Details error responses.
package problem

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

const typeBase = "https://api.civicplatform.ke/errors/"

// FieldError describes a single invalid request field.
type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

// Problem is the RFC 7807 response body with the platform's domain code.
type Problem struct {
	Type      string       `json:"type"`
	Title     string       `json:"title"`
	Status    int          `json:"status"`
	Code      string       `json:"code"`
	Detail    string       `json:"detail,omitempty"`
	Instance  string       `json:"instance,omitempty"`
	RequestID string       `json:"request_id,omitempty"`
	Errors    []FieldError `json:"errors,omitempty"`
}

// Write sends a problem response. code is the stable English domain code;
// detail is human-readable (localized in T-1.1.1.9).
func Write(w http.ResponseWriter, r *http.Request, status int, code, detail string, errs ...FieldError) {
	p := Problem{
		Type:      typeBase + code,
		Title:     http.StatusText(status),
		Status:    status,
		Code:      code,
		Detail:    detail,
		Instance:  r.URL.Path,
		RequestID: middleware.GetReqID(r.Context()),
		Errors:    errs,
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}
