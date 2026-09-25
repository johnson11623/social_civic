// Package httpjson decodes and encodes JSON request and response bodies.
package httpjson

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// MaxBodyBytes caps request bodies read by DecodeStrict.
const MaxBodyBytes = 64 << 10

// ErrMalformed is returned for bodies that are not a single valid JSON object
// matching the target type.
var ErrMalformed = errors.New("malformed JSON body")

// DecodeStrict decodes exactly one JSON object into dst, rejecting unknown
// fields, trailing data, and bodies larger than MaxBodyBytes.
func DecodeStrict(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.Join(ErrMalformed, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.Join(ErrMalformed, errors.New("unexpected data after JSON object"))
	}
	return nil
}

// Write sends v as a JSON response with the given status.
func Write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
