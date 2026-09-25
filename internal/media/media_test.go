package media

import (
	"strings"
	"testing"
)

func TestPolicy(t *testing.T) {
	for mime, want := range map[string]Kind{"image/jpeg": KindImage, " IMAGE/PNG ": KindImage, "video/mp4": KindVideo, "video/quicktime": KindVideo} {
		if k, ok := KindOf(mime); !ok || k != want {
			t.Errorf("%q: %v %v", mime, k, ok)
		}
	}
	for _, mime := range []string{"image/gif", "image/svg+xml", "application/pdf", "text/html"} {
		if _, ok := KindOf(mime); ok {
			t.Errorf("%q accepted", mime)
		}
	}
	if MaxBytes(KindImage) != 10<<20 || MaxBytes(KindVideo) != 100<<20 {
		t.Error("limits")
	}
	// Content must match: a script renamed .png is refused.
	if !sniffMatches(KindImage, "image/png", "image/png") || sniffMatches(KindImage, "image/png", "text/html; charset=utf-8") {
		t.Error("image sniff")
	}
	if !sniffMatches(KindVideo, "video/quicktime", "application/octet-stream") || sniffMatches(KindVideo, "video/mp4", "image/jpeg") {
		t.Error("video sniff")
	}
}

func TestScalingAndLadder(t *testing.T) {
	if h := scaledHeight(4000, 3000, 1080); h != 810 {
		t.Errorf("4:3 at 1080 = %d", h)
	}
	if h := scaledHeight(1001, 1001, 321); h%2 != 0 {
		t.Errorf("odd height %d", h)
	}
	if capWidth(800, 2048) != 800 || capWidth(4000, 320) != 320 {
		t.Error("capWidth")
	}
	names := func(rs []Rendition) string {
		var out []string
		for _, r := range rs {
			out = append(out, r.Name)
		}
		return strings.Join(out, ",")
	}
	if got := names(renditionsFor(Ladder, 720)); got != "240p,360p,480p,720p" {
		t.Errorf("720p source = %s", got)
	}
	if got := names(renditionsFor(Ladder, 144)); got != "240p" {
		t.Errorf("tiny source = %s", got)
	}
	master := masterPlaylist(renditionsFor(Ladder, 360), 640, 360)
	if !strings.HasPrefix(master, "#EXTM3U\n") || !strings.Contains(master, "RESOLUTION=426x240\n240p/playlist.m3u8") ||
		!strings.Contains(master, "BANDWIDTH=696000,RESOLUTION=640x360") {
		t.Errorf("master = %q", master)
	}
	if rs, err := ParseRenditions(" 240p, 480p "); err != nil || names(rs) != "240p,480p" {
		t.Errorf("parse = %v %v", rs, err)
	}
	if _, err := ParseRenditions("4k"); err == nil {
		t.Error("unknown rendition accepted")
	}
}

func TestPrefixedKeys(t *testing.T) {
	v := prefixed(Variants{Images: []ImageVariant{{WebP: "a.webp", JPEG: "a.jpg"}}, Poster: &ImageVariant{WebP: "p.webp", JPEG: "p.jpg"}, HLS: "master.m3u8"}, "id/")
	if v.Images[0].WebP != "id/a.webp" || v.Poster.JPEG != "id/p.jpg" || v.HLS != "id/master.m3u8" {
		t.Errorf("%+v", v)
	}
}
