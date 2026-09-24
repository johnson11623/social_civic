// Package problem writes RFC 7807 Problem Details error responses.
package problem

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/johnson11623/social_civic/internal/platform/i18n"
)

const typeBase = "https://api.civicplatform.ke/errors/"

// FieldError describes a single invalid request field. Codes are stable
// English identifiers, never translated.
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
	Lang      i18n.Lang    `json:"lang"`
	RequestID string       `json:"request_id,omitempty"`
	Errors    []FieldError `json:"errors,omitempty"`
}

// Write sends a problem response. code is the stable English domain code;
// title and detail are localized from the request's Accept-Language
// (default Kiswahili).
func Write(w http.ResponseWriter, r *http.Request, status int, code string, msg i18n.Key, errs ...FieldError) {
	lang := i18n.FromRequest(r)
	p := Problem{
		Type:      typeBase + code,
		Title:     i18n.StatusTitle(lang, status),
		Status:    status,
		Code:      code,
		Detail:    i18n.T(lang, msg),
		Instance:  r.URL.Path,
		Lang:      lang,
		RequestID: middleware.GetReqID(r.Context()),
		Errors:    errs,
	}
	h := w.Header()
	h.Set("Content-Type", "application/problem+json")
	h.Set("Content-Language", string(lang))
	h.Add("Vary", "Accept-Language")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}
