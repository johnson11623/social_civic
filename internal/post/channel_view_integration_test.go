package post

import (
	"encoding/json"
	"net/http"
	"testing"
)

// W2.1 support — ward header, read-only channels, channel post lists.

func TestChannels_ListNamesTheWard(t *testing.T) {
	e := newEnv(t)
	_, tok := e.user(t, kiamwangi, gatunduSouth, kiambu)
	e.user(t, kiamwangi, gatunduSouth, kiambu)
	if _, err := EnsureGeneralChannels(t.Context(), e.pool); err != nil {
		t.Fatal(err)
	}
	rec := e.do(t, "GET", "/v1/channels", tok, nil)
	var body struct {
		Ward        WardJSON      `json:"ward"`
		MemberCount int           `json:"member_count"`
		Items       []ChannelJSON `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Ward != (WardJSON{WardID: kiamwangi, Name: "Kiamwangi", Constituency: "Gatundu South", County: "Kiambu"}) ||
		body.MemberCount != 2 {
		t.Errorf("ward header = %+v, members %d", body.Ward, body.MemberCount)
	}
	if len(body.Items) != 1 || body.Items[0].Name != "general" || !body.Items[0].CanPost {
		t.Errorf("items = %+v", body.Items)
	}
}

func TestChannels_ReadOnlyOnlyCreatorPosts(t *testing.T) {
	e := newEnv(t)
	_, creator := e.user(t, kiamwangi, gatunduSouth, kiambu)
	_, member := e.user(t, kiamwangi, gatunduSouth, kiambu)
	rec := e.do(t, "POST", "/v1/channels", creator, map[string]any{"name": "mca-updates", "read_only": true})
	var ch ChannelJSON
	_ = json.Unmarshal(rec.Body.Bytes(), &ch)
	if rec.Code != http.StatusCreated || !ch.ReadOnly || !ch.CanPost {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if _, status := e.post(t, ch.ChannelID, creator, "Baraza on Friday at 10am"); status != http.StatusCreated {
		t.Errorf("creator post: %d", status)
	}
	if rec := e.do(t, "POST", "/v1/channels/"+ch.ChannelID+"/posts", member, map[string]any{"content": "hi"}); rec.Code != http.StatusForbidden || code(t, rec) != "read_only_channel" {
		t.Errorf("member post: %d %s", rec.Code, rec.Body)
	}
	var got ChannelJSON
	_ = json.Unmarshal(e.do(t, "GET", "/v1/channels/"+ch.ChannelID, member, nil).Body.Bytes(), &got)
	if !got.ReadOnly || got.CanPost {
		t.Errorf("member view = %+v", got)
	}
}

func TestChannels_PostsNewestFirstWithCursor(t *testing.T) {
	e := newEnv(t)
	channel, tok, _ := e.wardChannel(t, "water")
	other, _, _ := e.wardChannel(t, "roads")
	var ids []string
	for i := range 5 {
		p, _ := e.post(t, channel, tok, "water post "+string(rune('a'+i)))
		ids = append(ids, p.PostID)
	}
	e.post(t, other, tok, "a roads post")
	// Replies stay in their thread.
	e.do(t, "POST", "/v1/posts/"+ids[0]+"/replies", tok, map[string]any{"content": "reply"})
	e.do(t, "POST", "/v1/posts/"+ids[4]+"/likes", tok, nil)

	var page ChannelPostsResponse
	_ = json.Unmarshal(e.do(t, "GET", "/v1/channels/"+channel+"/posts?limit=3", tok, nil).Body.Bytes(), &page)
	if len(page.Items) != 3 || page.Items[0].PostID != ids[4] || page.Items[2].PostID != ids[2] || !page.HasMore {
		t.Fatalf("page 1 = %+v", page)
	}
	if l := page.Items[0].Liked; l == nil || !*l || page.Items[0].Channel != "water" {
		t.Errorf("first item = %+v", page.Items[0])
	}
	var next ChannelPostsResponse
	_ = json.Unmarshal(e.do(t, "GET", "/v1/channels/"+channel+"/posts?limit=3&cursor="+page.NextCursor, tok, nil).Body.Bytes(), &next)
	if len(next.Items) != 2 || next.Items[0].PostID != ids[1] || next.Items[1].PostID != ids[0] || next.HasMore {
		t.Errorf("page 2 = %+v", next)
	}

	_, elsewhere := e.user(t, 1400, 280, 47)
	if rec := e.do(t, "GET", "/v1/channels/"+channel+"/posts", elsewhere, nil); rec.Code != http.StatusForbidden {
		t.Errorf("other ward: %d", rec.Code)
	}
	if rec := e.do(t, "GET", "/v1/channels/"+channel+"/posts?cursor=%21", tok, nil); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("bad cursor: %d", rec.Code)
	}
}
