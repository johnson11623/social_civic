package moderation

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/johnson11623/social_civic/internal/membership"
	"github.com/johnson11623/social_civic/internal/post"
)

// Feature 3.1.3 — appeals.

type appealCase struct {
	e      *env
	author person
	mod    person
	post   string
	action ActionResponse
}

func newAppealCase(t *testing.T) appealCase {
	t.Helper()
	e := newEnv(t)
	c := appealCase{e: e, author: e.user(t, kiamwangi, gatunduSouth, kiambu)}
	c.mod = e.moderator(t, membership.RoleWardMod, 1, kiamwangi, kiamwangi, gatunduSouth, kiambu)
	c.post = e.post(t, c.author, post.LevelWard)
	var status int
	c.action, status, _ = e.act(t, c.mod, c.post, "hide", "hate_speech")
	if status != http.StatusCreated {
		t.Fatalf("hide: %d", status)
	}
	return c
}

func (c appealCase) file(t *testing.T, who person, actionID, statement string) (*AppealResponse, int, string) {
	t.Helper()
	rec := c.e.do(t, "POST", "/v1/appeals", who.token, map[string]any{"moderation_id": actionID, "statement": statement})
	var res AppealResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	return &res, rec.Code, code(rec)
}

func (c appealCase) committee(t *testing.T) person {
	t.Helper()
	m := c.e.user(t, 1366, 274, 47)
	if _, err := c.e.members.Appoint(context.Background(), m.Member, membership.RoleAppealsMember, 0, 0, 0, nil); err != nil {
		t.Fatal(err)
	}
	return m
}

func (c appealCase) decide(t *testing.T, who person, appealID, decision string) (DecisionResponse, int, string) {
	t.Helper()
	rec := c.e.do(t, "POST", "/v1/appeals/"+appealID+"/decision", who.token, map[string]any{"decision": decision, "notes": "Reviewed"})
	var res DecisionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	return res, rec.Code, code(rec)
}

func TestAppeals_FileWithinWindow(t *testing.T) {
	c := newAppealCase(t)
	e := c.e
	res, status, _ := c.file(t, c.author, c.action.ActionID, "I was quoting the chief's notice, not attacking anyone.")
	if status != http.StatusCreated || res.State != "open" || res.DueAt.Sub(e.now) < 13*24*time.Hour {
		t.Fatalf("file: %d %+v", status, res)
	}
	if n := e.count(t, `SELECT count(*) FROM outbox WHERE topic = 'appeal.filed'`); n != 1 {
		t.Error("appeal.filed missing")
	}
	if _, status, code := c.file(t, c.author, c.action.ActionID, "again"); status != 409 || code != "already_appealed" {
		t.Errorf("second appeal: %d %s", status, code)
	}
}

func TestAppeals_Refusals(t *testing.T) {
	c := newAppealCase(t)
	e := c.e
	other := e.user(t, kiamwangi, gatunduSouth, kiambu)
	if _, status, code := c.file(t, other, c.action.ActionID, "not mine"); status != 403 || code != "not_author" {
		t.Errorf("non-author: %d %s", status, code)
	}
	if _, status, _ := c.file(t, c.author, c.action.ActionID, "   "); status != 422 {
		t.Errorf("empty statement: %d", status)
	}
	if _, status, _ := c.file(t, c.author, uuid.NewString(), "x"); status != 404 {
		t.Errorf("unknown action: %d", status)
	}
	// T-3.1.3.2 — day 15 is too late.
	e.now = e.now.Add(15 * 24 * time.Hour)
	if _, status, code := c.file(t, c.author, c.action.ActionID, "late"); status != 409 || code != "outside_window" {
		t.Errorf("day 15: %d %s", status, code)
	}
	// A restore isn't a decision against the author.
	e.now = time.Now()
	restore, _, _ := e.act(t, c.mod, c.post, "restore", "hate_speech")
	if _, status, code := c.file(t, c.author, restore.ActionID, "x"); status != 409 || code != "not_appealable" {
		t.Errorf("restore: %d %s", status, code)
	}
}

func TestAppeals_OverturnRestoresAndFlags(t *testing.T) {
	c := newAppealCase(t)
	e := c.e
	appeal, _, _ := c.file(t, c.author, c.action.ActionID, "I was quoting the chief's notice.")
	member := c.committee(t)

	if _, status, _ := c.decide(t, c.author, appeal.AppealID, "overturn"); status != 403 {
		t.Errorf("author deciding: %d", status)
	}
	var list struct{ Items []AppealJSON }
	_ = json.Unmarshal(e.do(t, "GET", "/v1/appeals", member.token, nil).Body.Bytes(), &list)
	if len(list.Items) != 1 || list.Items[0].AppealID != appeal.AppealID || list.Items[0].ReasonCode != "hate_speech" || list.Items[0].Content == nil {
		t.Errorf("open appeals = %+v", list.Items)
	}

	res, status, _ := c.decide(t, member, appeal.AppealID, "overturn")
	if status != http.StatusOK || res.State != "overturned" || res.PostState != "active" {
		t.Fatalf("overturn: %d %+v", status, res)
	}
	if n := e.count(t, "SELECT count(*) FROM posts WHERE public_id = $1 AND state = 1 AND deleted_at IS NULL", c.post); n != 1 {
		t.Error("post not restored")
	}
	// The moderator's decision is flagged; the restore is on the record.
	if n := e.count(t, "SELECT count(*) FROM moderation_actions WHERE public_id = $1 AND overturned_at IS NOT NULL", c.action.ActionID); n != 1 {
		t.Error("action not flagged as overturned")
	}
	if n := e.count(t, "SELECT count(*) FROM moderation_actions WHERE action = 'restore' AND actor_id = $1", member.ID); n != 1 {
		t.Error("restore not recorded")
	}
	if n := e.count(t, `SELECT count(*) FROM outbox WHERE topic = 'appeal.resolved' AND payload->'data'->>'decision' = 'overturn'
		AND (payload->'data'->>'moderator_id')::bigint = $1`, c.mod.ID); n != 1 {
		t.Error("appeal.resolved missing")
	}
	if len(e.cache.bumped) < 2 {
		t.Errorf("feed not invalidated on restore: %v", e.cache.bumped)
	}
	if _, status, code := c.decide(t, member, appeal.AppealID, "uphold"); status != 409 || code != "appeal_decided" {
		t.Errorf("second decision: %d %s", status, code)
	}
	// Once overturned, the decision can't be appealed again.
	if _, status, code := c.file(t, c.author, c.action.ActionID, "again"); status != 409 {
		t.Errorf("appeal after overturn: %d %s", status, code)
	}
}

func TestAppeals_UpholdAndNoSelfReview(t *testing.T) {
	c := newAppealCase(t)
	e := c.e
	appeal, _, _ := c.file(t, c.author, c.action.ActionID, "Please review.")
	// The moderator who acted can't sit on their own appeal.
	if _, err := e.members.Appoint(context.Background(), c.mod.Member, membership.RoleAppealsMember, 0, 0, 0, nil); err != nil {
		t.Fatal(err)
	}
	if _, status, _ := c.decide(t, c.mod, appeal.AppealID, "overturn"); status != 403 {
		t.Errorf("self review: %d", status)
	}
	member := c.committee(t)
	if _, status, code := c.decide(t, member, appeal.AppealID, "maybe"); status != 422 || code != "invalid_decision" {
		t.Errorf("invalid decision: %d %s", status, code)
	}
	res, status, _ := c.decide(t, member, appeal.AppealID, "uphold")
	if status != http.StatusOK || res.State != "upheld" || res.PostState != "tombstoned" {
		t.Errorf("uphold: %d %+v", status, res)
	}
	if n := e.count(t, "SELECT count(*) FROM moderation_actions WHERE overturned_at IS NOT NULL"); n != 0 {
		t.Error("upheld action flagged")
	}
}
