package moderation

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/johnson11623/social_civic/internal/membership"
	"github.com/johnson11623/social_civic/internal/post"
)

// Feature 3.1.2 — moderation actions, queue and history.

func (e *env) act(t *testing.T, mod person, postID, action, reason string) (ActionResponse, int, string) {
	t.Helper()
	rec := e.do(t, "POST", "/v1/moderation/actions", mod.token, map[string]any{
		"post_id": postID, "action": action, "reason_code": reason, "notes": "Targeted slur",
	})
	var res ActionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	return res, rec.Code, code(rec)
}

func TestActions_WardModeratorHidesPost(t *testing.T) {
	e := newEnv(t)
	author := e.user(t, kiamwangi, gatunduSouth, kiambu)
	reporter := e.user(t, kiamwangi, gatunduSouth, kiambu)
	mod := e.moderator(t, membership.RoleWardMod, 1, kiamwangi, kiamwangi, gatunduSouth, kiambu)
	p := e.post(t, author, post.LevelWard)
	e.do(t, "POST", "/v1/reports", reporter.token, map[string]any{"post_id": p, "reason_code": "hate_speech"})

	res, status, _ := e.act(t, mod, p, "hide", "hate_speech")
	if status != http.StatusCreated || res.PostState != "tombstoned" || res.AppealWindowHours != 336 ||
		res.AppealDueAt == nil || res.AppealDueAt.Sub(e.now) < 13*24*time.Hour {
		t.Fatalf("act: %d %+v", status, res)
	}
	if n := e.count(t, "SELECT count(*) FROM posts WHERE public_id = $1 AND state = 3 AND deleted_at IS NOT NULL AND content IS NOT NULL", p); n != 1 {
		t.Error("post not tombstoned (content kept for appeal)")
	}
	if n := e.count(t, "SELECT count(*) FROM reports WHERE state = 2 AND resolved_at IS NOT NULL"); n != 1 {
		t.Error("open report not marked actioned")
	}
	// T-3.1.2.4 — the event, with the author for notification.
	if n := e.count(t, `SELECT count(*) FROM outbox WHERE topic = 'moderation.action_recorded'
		AND payload->'data'->>'action' = 'hide' AND (payload->'data'->>'author_id')::bigint = $1`, author.ID); n != 1 {
		t.Error("moderation.action_recorded missing")
	}
	// T-3.1.2.8 — the feed that showed it is invalidated.
	if len(e.cache.bumped) != 1 || e.cache.bumped[0] != (post.Scope{Level: post.LevelWard, ID: kiamwangi}) {
		t.Errorf("cache bumps = %v", e.cache.bumped)
	}
	// Viewers now get the decision, not the content.
	got, err := post.NewStore(e.pool).PostByPublicID(t.Context(), uuid.MustParse(p))
	if err != nil || got.Moderation == nil || got.Moderation.ReasonCode != "hate_speech" || got.Moderation.ActionID != res.ActionID {
		t.Errorf("post moderation = %+v, %v", got.Moderation, err)
	}
	// Hiding twice is refused.
	if _, status, c := e.act(t, mod, p, "hide", "hate_speech"); status != http.StatusConflict || c != "already_moderated" {
		t.Errorf("second hide: %d %s", status, c)
	}
}

func TestActions_TieredAuthority(t *testing.T) {
	e := newEnv(t)
	author := e.user(t, kiamwangi, gatunduSouth, kiambu)
	wardMod := e.moderator(t, membership.RoleWardMod, 1, kiamwangi, kiamwangi, gatunduSouth, kiambu)
	neighbourMod := e.moderator(t, membership.RoleWardMod, 1, 552, 552, gatunduSouth, kiambu)
	countyMod := e.moderator(t, membership.RoleCountyMod, 3, kiambu, 559, 113, kiambu)
	resident := e.user(t, kiamwangi, gatunduSouth, kiambu)
	national := e.post(t, author, post.LevelNational)
	county := e.post(t, author, post.LevelCounty)
	ward := e.post(t, author, post.LevelWard)

	for name, c := range map[string]struct {
		mod  person
		post string
	}{
		"ward mod on national post": {wardMod, national},
		"ward mod on county post":   {wardMod, county},
		"other ward's mod":          {neighbourMod, ward},
		"no role":                   {resident, ward},
		"county mod on national":    {countyMod, national},
	} {
		if _, status, code := e.act(t, c.mod, c.post, "delete", "incitement"); status != http.StatusForbidden || code != "insufficient_authority" {
			t.Errorf("%s: %d %s", name, status, code)
		}
	}
	if _, status, _ := e.act(t, countyMod, ward, "freeze", "incitement"); status != http.StatusCreated {
		t.Errorf("county mod on a ward post in the county: %d", status)
	}
	if _, status, _ := e.act(t, countyMod, county, "delete", "incitement"); status != http.StatusCreated {
		t.Errorf("county mod on a county post: %d", status)
	}
}

func TestActions_FreezeRestoreAndValidation(t *testing.T) {
	e := newEnv(t)
	author := e.user(t, kiamwangi, gatunduSouth, kiambu)
	mod := e.moderator(t, membership.RoleWardMod, 1, kiamwangi, kiamwangi, gatunduSouth, kiambu)
	p := e.post(t, author, post.LevelWard)

	if _, status, c := e.act(t, mod, p, "ban", "incitement"); status != 422 || c != "invalid_action" {
		t.Errorf("invalid action: %d %s", status, c)
	}
	if _, status, c := e.act(t, mod, p, "hide", "rude"); status != 422 || c != "invalid_reason" {
		t.Errorf("invalid reason: %d %s", status, c)
	}
	if _, status, _ := e.act(t, mod, p, "restore", "incitement"); status != http.StatusConflict {
		t.Errorf("restore active: %d", status)
	}
	res, status, _ := e.act(t, mod, p, "freeze", "incitement")
	if status != http.StatusCreated || res.PostState != "frozen" {
		t.Fatalf("freeze: %d %+v", status, res)
	}
	res, status, _ = e.act(t, mod, p, "restore", "incitement")
	if status != http.StatusCreated || res.PostState != "active" || res.AppealDueAt != nil {
		t.Errorf("restore: %d %+v", status, res)
	}
	if n := e.count(t, "SELECT count(*) FROM posts WHERE public_id = $1 AND state = 1 AND deleted_at IS NULL", p); n != 1 {
		t.Error("post not restored")
	}
}

func TestQueue_ScopedSortedAndFiltered(t *testing.T) {
	e := newEnv(t)
	author := e.user(t, kiamwangi, gatunduSouth, kiambu)
	far := e.user(t, 1366, 274, 47)
	mod := e.moderator(t, membership.RoleWardMod, 1, kiamwangi, kiamwangi, gatunduSouth, kiambu)
	natMod := e.moderator(t, membership.RoleNatMod, 4, membership.NationalUnit, 1366, 274, 47)
	quiet := e.post(t, author, post.LevelWard)
	busy := e.post(t, author, post.LevelWard)
	elevated := e.post(t, author, post.LevelCounty) // beyond the ward mod
	nairobi := e.post(t, far, post.LevelWard)

	report := func(p string, reporter person) {
		if rec := e.do(t, "POST", "/v1/reports", reporter.token, map[string]any{"post_id": p, "reason_code": "privacy"}); rec.Code != 201 {
			t.Fatalf("report: %d %s", rec.Code, rec.Body)
		}
	}
	report(quiet, e.user(t, kiamwangi, gatunduSouth, kiambu))
	for range 3 {
		report(busy, e.user(t, kiamwangi, gatunduSouth, kiambu))
	}
	report(elevated, e.user(t, kiamwangi, gatunduSouth, kiambu))
	report(nairobi, e.user(t, 1366, 274, 47))

	queue := func(tok, query string) []QueueItem {
		t.Helper()
		rec := e.do(t, "GET", "/v1/moderation/queue"+query, tok, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("queue%s: %d %s", query, rec.Code, rec.Body)
		}
		var body struct{ Items []QueueItem }
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return body.Items
	}
	ids := func(items []QueueItem) (out []string) {
		for _, i := range items {
			out = append(out, i.PostID)
		}
		return
	}

	// Oldest report first by default; only the ward mod's own ward, at ward level.
	if got := ids(queue(mod.token, "")); len(got) != 2 || got[0] != quiet || got[1] != busy {
		t.Errorf("ward queue = %v", got)
	}
	items := queue(mod.token, "?sort=reports")
	if items[0].PostID != busy || items[0].ReportCount != 3 || items[0].Reasons[0] != "privacy" || items[0].Content == nil {
		t.Errorf("by reports = %+v", items)
	}
	if got := queue(natMod.token, ""); len(got) != 4 {
		t.Errorf("national queue = %v", ids(got))
	}
	if got := ids(queue(natMod.token, "?level=county")); len(got) != 1 || got[0] != elevated {
		t.Errorf("county level = %v", got)
	}
	// Triage: frozen posts; acted-on reports leave the open queue.
	e.act(t, mod, busy, "freeze", "privacy")
	if got := ids(queue(mod.token, "?filter=triage")); len(got) != 0 {
		t.Errorf("triage after freeze (reports resolved) = %v", got)
	}
	if got := ids(queue(mod.token, "")); len(got) != 1 || got[0] != quiet {
		t.Errorf("open after freeze = %v", got)
	}
	if rec := e.do(t, "GET", "/v1/moderation/queue", author.token, nil); rec.Code != http.StatusForbidden {
		t.Errorf("non-moderator: %d", rec.Code)
	}
	if rec := e.do(t, "GET", "/v1/moderation/queue?filter=everything", mod.token, nil); rec.Code != 422 {
		t.Errorf("bad filter: %d", rec.Code)
	}
}

func TestHistory_ModeratorsSeeNotesAuthorsSeeDecisions(t *testing.T) {
	e := newEnv(t)
	author := e.user(t, kiamwangi, gatunduSouth, kiambu)
	bystander := e.user(t, kiamwangi, gatunduSouth, kiambu)
	mod := e.moderator(t, membership.RoleWardMod, 1, kiamwangi, kiamwangi, gatunduSouth, kiambu)
	p := e.post(t, author, post.LevelWard)
	e.act(t, mod, p, "freeze", "incitement")
	e.act(t, mod, p, "delete", "incitement")

	history := func(tok string) (int, []ActionJSON) {
		rec := e.do(t, "GET", "/v1/posts/"+p+"/moderation", tok, nil)
		var body struct{ Items []ActionJSON }
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		return rec.Code, body.Items
	}
	status, items := history(mod.token)
	if status != 200 || len(items) != 2 || items[0].Action != "delete" || items[0].Notes != "Targeted slur" || items[0].Moderator == nil {
		t.Errorf("moderator view: %d %+v", status, items)
	}
	status, items = history(author.token)
	if status != 200 || len(items) != 2 || items[0].Notes != "" || items[0].Moderator != nil || items[0].AppealDueAt == nil {
		t.Errorf("author view: %d %+v", status, items)
	}
	if status, _ := history(bystander.token); status != http.StatusForbidden {
		t.Errorf("bystander: %d", status)
	}
}
