// Package i18n resolves the caller's language (English or Kiswahili) and
// translates user-facing messages (T-1.1.1.9, T-X.2).
//
// Messages live in locales/<lang>.json. Every key must exist in every
// catalog; the package panics at start-up otherwise, and the tests check it.
// Machine-readable codes (error codes, reason codes) are never translated.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"golang.org/x/text/language"
)

// Lang is a supported language code.
type Lang string

const (
	Swahili Lang = "sw"
	English Lang = "en"

	// Default is used when the caller states no supported preference
	// (LLD v2.0 §10: fallback sw).
	Default = Swahili
)

// Supported lists the languages in catalog order.
var Supported = []Lang{Swahili, English}

// Key identifies a translatable message.
type Key string

// Message keys. Keep in sync with locales/*.json (enforced by tests).
const (
	MsgMalformedJSON    Key = "error.malformed_json"
	MsgValidationFailed Key = "error.validation_failed"
	MsgInternal         Key = "error.internal"
	MsgRateLimited      Key = "error.rate_limited"
	MsgUnauthenticated  Key = "error.unauthenticated"

	MsgInvalidNationalID       Key = "identity.invalid_id"
	MsgConsentRequired         Key = "identity.consent_required"
	MsgUnknownWard             Key = "identity.unknown_ward"
	MsgIDAlreadyRegistered     Key = "identity.id_already_registered"
	MsgRegistrationUnavailable Key = "identity.registration_unavailable"
	MsgInvalidPhone            Key = "identity.invalid_phone"
	MsgInvalidCredentials      Key = "identity.invalid_credentials"
	MsgTokenInvalid            Key = "identity.token_invalid"
	MsgTokenExpired            Key = "identity.token_expired"
	MsgTokenRevoked            Key = "identity.token_revoked"
	MsgTokenReuseDetected      Key = "identity.token_reuse_detected"
	MsgConsentNotFound         Key = "identity.consent_not_found"
	MsgConsentWithdrawn        Key = "identity.consent_withdrawn"
	MsgErasureInProgress       Key = "identity.erasure_in_progress"

	// SMSLoginCode takes the code (%s) and its lifetime in minutes (%d).
	MsgSMSLoginCode Key = "sms.login_code"

	MsgNotMember           Key = "post.not_member"
	MsgChannelNotFound     Key = "post.channel_not_found"
	MsgInvalidChannelName  Key = "post.invalid_channel_name"
	MsgChannelNameReserved Key = "post.channel_name_reserved"
	MsgChannelNameTaken    Key = "post.channel_name_taken"
	MsgInvalidCategory     Key = "post.invalid_category"
	MsgDescriptionTooLong  Key = "post.description_too_long"
	MsgContentEmpty        Key = "post.content_empty"
	MsgContentTooLong      Key = "post.content_too_long"
	MsgMediaNotSupported   Key = "post.media_not_supported"
	MsgReadOnlyChannel     Key = "post.read_only_channel"
	MsgPostNotFound        Key = "post.not_found"
	MsgOutOfScope          Key = "post.out_of_scope"
	MsgAlreadyLiked        Key = "post.already_liked"
	MsgLikeNotFound        Key = "post.like_not_found"
	MsgPostNotActive       Key = "post.not_active"

	MsgInsufficientAuthority Key = "membership.insufficient_authority"
	MsgAlreadyAssigned       Key = "membership.already_assigned"
	MsgInvalidRole           Key = "membership.invalid_role"
	MsgInvalidUnit           Key = "membership.invalid_unit"
	MsgNotInGroup            Key = "membership.not_in_group"
	MsgUserNotFound          Key = "membership.user_not_found"
	MsgInvalidTerm           Key = "membership.invalid_term"

	MsgSearchTooShort Key = "boundary.search_too_short"
	MsgSearchTooLong  Key = "boundary.search_too_long"
	MsgInvalidLevel   Key = "boundary.invalid_level"
	MsgInvalidLimit   Key = "boundary.invalid_limit"
)

//go:embed locales/*.json
var localeFS embed.FS

var (
	catalogs = mustLoad()
	matcher  = language.NewMatcher([]language.Tag{language.Swahili, language.English}) // first = default
)

func mustLoad() map[Lang]map[Key]string {
	out := make(map[Lang]map[Key]string, len(Supported))
	for _, l := range Supported {
		raw, err := localeFS.ReadFile("locales/" + string(l) + ".json")
		if err != nil {
			panic(fmt.Sprintf("i18n: missing catalog %s: %v", l, err))
		}
		var m map[Key]string
		if err := json.Unmarshal(raw, &m); err != nil {
			panic(fmt.Sprintf("i18n: invalid catalog %s: %v", l, err))
		}
		out[l] = m
	}
	if missing := missingKeys(out); len(missing) > 0 {
		panic(fmt.Sprintf("i18n: catalogs out of sync: %v", missing))
	}
	return out
}

// missingKeys reports keys present in one catalog but not another.
func missingKeys(cats map[Lang]map[Key]string) []string {
	all := map[Key]bool{}
	for _, m := range cats {
		for k := range m {
			all[k] = true
		}
	}
	var missing []string
	for _, l := range Supported {
		for k := range all {
			if v, ok := cats[l][k]; !ok || v == "" {
				missing = append(missing, fmt.Sprintf("%s:%s", l, k))
			}
		}
	}
	sort.Strings(missing)
	return missing
}

// FromRequest picks the best supported language from Accept-Language
// (e.g. "sw-KE", "en-GB,en;q=0.9"), falling back to Default.
func FromRequest(r *http.Request) Lang {
	return Parse(r.Header.Get("Accept-Language"))
}

// Parse matches an Accept-Language value against the supported languages.
func Parse(acceptLanguage string) Lang {
	if acceptLanguage == "" {
		return Default
	}
	_, index, confidence := matcher.Match(parseTags(acceptLanguage)...)
	if confidence == language.No {
		return Default
	}
	return Supported[index]
}

func parseTags(acceptLanguage string) []language.Tag {
	tags, _, err := language.ParseAcceptLanguage(acceptLanguage)
	if err != nil {
		return nil
	}
	return tags
}

// T returns the message for key in lang, falling back to Default, then to the key itself.
func T(lang Lang, key Key) string {
	if m, ok := catalogs[lang]; ok {
		if s, ok := m[key]; ok {
			return s
		}
	}
	if s, ok := catalogs[Default][key]; ok {
		return s
	}
	return string(key)
}

// StatusTitle returns the localized title for an HTTP status.
func StatusTitle(lang Lang, status int) string {
	key := Key(fmt.Sprintf("status.%d", status))
	if s, ok := catalogs[lang][key]; ok {
		return s
	}
	return http.StatusText(status)
}
