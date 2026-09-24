package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/johnson11623/social_civic/internal/boundary"
	"github.com/johnson11623/social_civic/internal/platform/problem"
	"github.com/johnson11623/social_civic/pkg/events"
	"github.com/johnson11623/social_civic/pkg/kms"
)

var fixedNow = time.Date(2026, 3, 1, 8, 15, 0, 0, time.UTC)

const testPepper = "test-pepper-0123456789abcdef0123"

// fakeStore records users and the events that would be enqueued with them.
// Like the real store, it enqueues an event only when the write succeeds.
type fakeStore struct {
	calls  []NewUser
	events []events.Event
	err    error
}

func (f *fakeStore) CreateUser(_ context.Context, u NewUser, event func(CreatedUser) events.Event) (CreatedUser, error) {
	f.calls = append(f.calls, u)
	if f.err != nil {
		return CreatedUser{}, f.err
	}
	created := CreatedUser{ID: 42, PublicID: u.PublicID, CreatedAt: u.ConsentAt}
	f.events = append(f.events, event(created))
	return created, nil
}

type failingKeyring struct{}

func (failingKeyring) Current(context.Context, string) (kms.Key, error) {
	return kms.Key{}, errors.New("vault sealed")
}

type fixture struct {
	handler *RegisterHandler
	store   *fakeStore
	tokens  *TokenIssuer
}

// testTree is the real IEBC boundary set, so tests use real ward codes.
func testTree(t *testing.T) *boundary.Tree {
	t.Helper()
	units, err := boundary.LoadIEBCCSVFile("../../db/seed/iebc_2022_wards.csv")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := boundary.NewTree(boundary.IEBC2022, units)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	keyring := kms.NewStatic()
	keyring.Set(PepperKeyName, []byte(testPepper), "v7")
	tokens, err := NewTokenIssuer([]byte("test-signing-key-0123456789abcdef"), func() time.Time { return fixedNow })
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{store: &fakeStore{}, tokens: tokens}
	f.handler = &RegisterHandler{
		Store:    f.store,
		Keyring:  keyring,
		Boundary: testTree(t),
		Tokens:   tokens,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:      func() time.Time { return fixedNow },
	}
	return f
}

func validBody() map[string]any {
	return map[string]any{
		"national_id":     "12345678",
		"display_name":    "Wanjiku M.",
		"preferred_lang":  "sw",
		"ward_id":         551, // Kiamwangi, Gatundu South, Kiambu
		"consent_version": "2026-01",
		"consent_granted": true,
	}
}

func (f *fixture) post(t *testing.T, body any) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	switch b := body.(type) {
	case string:
		raw = []byte(b)
	default:
		var err error
		if raw, err = json.Marshal(b); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/register", bytes.NewReader(raw))
	req.RemoteAddr = "196.201.214.10:51234"
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) problem.Problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	var p problem.Problem
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRegister_Success(t *testing.T) {
	f := newFixture(t)
	rec := f.post(t, validBody())

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var resp RegisterResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}

	wantGroups := []Group{
		{Level: 1, ID: 551, Name: "Kiamwangi"},
		{Level: 2, ID: 111, Name: "Gatundu South"},
		{Level: 3, ID: 22, Name: "Kiambu"},
		{Level: 4, ID: 1, Name: "National"},
	}
	if len(resp.Groups) != 4 {
		t.Fatalf("groups = %+v", resp.Groups)
	}
	for i, g := range wantGroups {
		if resp.Groups[i] != g {
			t.Errorf("groups[%d] = %+v, want %+v", i, resp.Groups[i], g)
		}
	}
	if resp.UserID == "" || resp.UserID != resp.PublicID {
		t.Errorf("user_id %q should equal public_id %q", resp.UserID, resp.PublicID)
	}
	if resp.ExpiresIn != 900 {
		t.Errorf("expires_in = %d, want 900", resp.ExpiresIn)
	}

	// Tokens are valid, typed, and carry jti/aud/iss/nbf (F-03).
	for typ, tok := range map[string]string{"access": resp.AccessToken, "refresh": resp.RefreshToken} {
		c, err := f.tokens.Parse(tok)
		if err != nil {
			t.Fatalf("%s token invalid: %v", typ, err)
		}
		if c.Type != typ || c.Subject != resp.PublicID || c.ID == "" || c.NotBefore == nil {
			t.Errorf("%s claims = %+v", typ, c)
		}
	}

	// Store received the hash and key version — never the raw ID.
	if len(f.store.calls) != 1 {
		t.Fatalf("store calls = %d", len(f.store.calls))
	}
	u := f.store.calls[0]
	if len(u.NationalIDHash) != 32 || bytes.Contains(u.NationalIDHash, []byte("12345678")) {
		t.Errorf("national ID hash looks wrong: %x", u.NationalIDHash)
	}
	if !bytes.Equal(u.NationalIDHash, hashNationalID("12345678", []byte(testPepper))) {
		t.Error("hash is not the HMAC of the national ID with the current pepper")
	}
	if u.KeyVersion != "v7" || u.ConsentVersion != "2026-01" || !u.ConsentAt.Equal(fixedNow) {
		t.Errorf("stored user = %+v", u)
	}
	if u.WardID != 551 || u.ConstituencyID != 111 || u.CountyID != 22 {
		t.Errorf("scope = %d/%d/%d", u.WardID, u.ConstituencyID, u.CountyID)
	}
	if len(u.IPHash) != 32 {
		t.Errorf("ip hash length = %d", len(u.IPHash))
	}

	// Exactly one user.registered event enqueued with the user, with no national ID material.
	evts := f.store.events
	if len(evts) != 1 || evts[0].Type != EventUserRegistered || evts[0].SpecVersion != "1.0" || evts[0].Source != "identity" || !evts[0].Time.Equal(fixedNow) {
		t.Fatalf("events = %+v", evts)
	}
	payload, _ := json.Marshal(evts[0])
	if strings.Contains(string(payload), "12345678") || strings.Contains(string(payload), "national_id") {
		t.Errorf("event leaks national ID: %s", payload)
	}
	data := evts[0].Data.(UserRegisteredData)
	if data.UserID != 42 || data.WardID != 551 || data.PublicID != resp.PublicID {
		t.Errorf("event data = %+v", data)
	}
}

func TestRegister_DefaultsLanguageToKiswahili(t *testing.T) {
	f := newFixture(t)
	body := validBody()
	delete(body, "preferred_lang")
	rec := f.post(t, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if got := f.store.calls[0].PreferredLang; got != "sw" {
		t.Errorf("preferred_lang = %q, want sw", got)
	}
}

func TestRegister_Rejections(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(map[string]any)
		wantStatus int
		wantCode   string
		wantField  string
	}{
		{"7-digit ID", func(b map[string]any) { b["national_id"] = "1234567" }, 400, "invalid_id", ""},
		{"9-digit ID", func(b map[string]any) { b["national_id"] = "123456789" }, 400, "invalid_id", ""},
		{"ID with letter", func(b map[string]any) { b["national_id"] = "1234567a" }, 400, "invalid_id", ""},
		{"missing ID", func(b map[string]any) { delete(b, "national_id") }, 400, "invalid_id", ""},
		{"consent false", func(b map[string]any) { b["consent_granted"] = false }, 422, "consent_required", ""},
		{"consent missing", func(b map[string]any) { delete(b, "consent_granted") }, 422, "consent_required", ""},
		{"consent version empty", func(b map[string]any) { b["consent_version"] = " " }, 422, "validation_failed", "consent_version"},
		{"blank display name", func(b map[string]any) { b["display_name"] = "   " }, 422, "validation_failed", "display_name"},
		{"display name too long", func(b map[string]any) { b["display_name"] = strings.Repeat("ñ", 101) }, 422, "validation_failed", "display_name"},
		{"unsupported language", func(b map[string]any) { b["preferred_lang"] = "fr" }, 422, "validation_failed", "preferred_lang"},
		{"missing ward", func(b map[string]any) { delete(b, "ward_id") }, 422, "validation_failed", "ward_id"},
		{"unknown ward", func(b map[string]any) { b["ward_id"] = 9999 }, 422, "invalid_unit", "ward_id"},
		{"ward code past 1450", func(b map[string]any) { b["ward_id"] = 1451 }, 422, "invalid_unit", "ward_id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			body := validBody()
			tt.mutate(body)
			rec := f.post(t, body)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tt.wantStatus, rec.Body)
			}
			p := decodeProblem(t, rec)
			if p.Code != tt.wantCode {
				t.Errorf("code = %q, want %q", p.Code, tt.wantCode)
			}
			if tt.wantField != "" && (len(p.Errors) == 0 || p.Errors[0].Field != tt.wantField) {
				t.Errorf("errors = %+v, want field %q", p.Errors, tt.wantField)
			}
			if len(f.store.calls) != 0 {
				t.Error("rejected request must not reach the store")
			}
			if len(f.store.events) != 0 {
				t.Error("rejected request must not publish events")
			}
		})
	}
}

func TestRegister_MalformedBodies(t *testing.T) {
	for name, body := range map[string]string{
		"not json":      "national_id=12345678",
		"unknown field": `{"national_id":"12345678","display_name":"W","ward_id":551,"consent_version":"2026-01","consent_granted":true,"is_admin":true}`,
		"two objects":   `{"national_id":"12345678"}{"national_id":"87654321"}`,
		"wrong type":    `{"national_id":12345678}`,
		"empty":         ``,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			rec := f.post(t, body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
			}
			if p := decodeProblem(t, rec); p.Code != "malformed_json" {
				t.Errorf("code = %q", p.Code)
			}
		})
	}
}

func TestRegister_DuplicateNationalID(t *testing.T) {
	f := newFixture(t)
	f.store.err = ErrDuplicateNationalID
	rec := f.post(t, validBody())
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d", rec.Code)
	}
	if p := decodeProblem(t, rec); p.Code != "id_already_registered" {
		t.Errorf("code = %q", p.Code)
	}
	if len(f.store.events) != 0 {
		t.Error("duplicate must not publish events")
	}
}

func TestRegister_KeyringUnavailable(t *testing.T) {
	f := newFixture(t)
	f.handler.Keyring = failingKeyring{}
	rec := f.post(t, validBody())
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
	if p := decodeProblem(t, rec); p.Code != "kms_unavailable" {
		t.Errorf("code = %q", p.Code)
	}
	if len(f.store.calls) != 0 {
		t.Error("must not store without a pepper")
	}
}

func TestRegister_StoreFailureHidesDetails(t *testing.T) {
	f := newFixture(t)
	f.store.err = errors.New(`pq: connection refused to 10.0.0.5`)
	rec := f.post(t, validBody())
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Error("internal error details leaked to client")
	}
}

func TestValidateNationalID(t *testing.T) {
	for id, ok := range map[string]bool{
		"12345678": true, "00000000": true,
		"": false, "1234567": false, "123456789": false, "1234567a": false,
		"１２３４５６７８": false, // full-width digits
		"1234 678": false,
	} {
		if err := ValidateNationalID(id); (err == nil) != ok {
			t.Errorf("ValidateNationalID(%q) err = %v, want ok=%v", id, err, ok)
		}
	}
}

func TestNewTokenIssuer_RejectsShortKey(t *testing.T) {
	if _, err := NewTokenIssuer([]byte("short"), nil); err == nil {
		t.Error("expected error for short signing key")
	}
}
