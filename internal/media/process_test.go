package media

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Real ffmpeg on synthetic media (docs/media: synthetic fixtures only).
// Needs ffmpeg with libx264; WebP variants are checked when it has libwebp
// (the worker container does: make test-media).

func requireFFmpeg(t *testing.T) {
	t.Helper()
	out, err := exec.Command("ffmpeg", "-hide_banner", "-encoders").Output()
	if err != nil || !bytes.Contains(out, []byte("libx264")) {
		t.Skip("ffmpeg with libx264 not available")
	}
}

// perVariant is the files per image size: JPEG, plus WebP when available.
func perVariant(p *Processor) int {
	if p.hasWebP(context.Background()) {
		return 2
	}
	return 1
}

// jpegWithGPS returns a JPEG carrying an EXIF block with a GPS marker string.
func jpegWithGPS(t *testing.T, w, h int) ([]byte, string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 255 / w), uint8(y * 255 / h), 120, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	const secret = "GPS-1.2921S-36.8219E-SECRET"
	payload := append([]byte("Exif\x00\x00MM\x00\x2a\x00\x00\x00\x08\x00\x00"), []byte(secret)...)
	app1 := []byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	raw := buf.Bytes()
	out := append(append(append([]byte{}, raw[:2]...), append(app1, payload...)...), raw[2:]...) // after SOI
	return out, secret
}

func TestProcessImage_VariantsWithoutMetadata(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	data, secret := jpegWithGPS(t, 1600, 1200)
	if !bytes.Contains(data, []byte(secret)) {
		t.Fatal("fixture lacks its marker")
	}
	in := filepath.Join(dir, "in.jpg")
	if err := os.WriteFile(in, data, 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	_ = os.Mkdir(out, 0o755)
	proc := &Processor{}
	res, err := proc.Image(context.Background(), in, out)
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 1600 || res.Height != 1200 || len(res.Variants.Images) != 3 || len(res.Files) != 3*perVariant(proc) {
		t.Fatalf("result = %+v", res)
	}
	widths := map[string]int{}
	for _, v := range res.Variants.Images {
		widths[v.Name] = v.Width
	}
	// Never enlarged: large is capped at the source width.
	if widths["thumbnail"] != 320 || widths["medium"] != 1080 || widths["large"] != 1600 {
		t.Errorf("widths = %v", widths)
	}
	for _, f := range res.Files {
		b, err := os.ReadFile(f.Path)
		if err != nil || len(b) == 0 {
			t.Fatalf("%s: %v", f.Key, err)
		}
		if bytes.Contains(b, []byte(secret)) || bytes.Contains(b, []byte("Exif\x00")) {
			t.Errorf("%s still carries EXIF", f.Key)
		}
	}
	if !strings.HasPrefix(res.Placeholder, "data:image/") || len(res.Placeholder) > 2000 {
		t.Errorf("placeholder = %d bytes", len(res.Placeholder))
	}
}

func TestProcessImage_RejectsNonImages(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "in.png")
	_ = os.WriteFile(in, []byte("<script>alert(1)</script>"), 0o644)
	if _, err := (&Processor{}).Image(context.Background(), in, dir); err == nil || !strings.Contains(err.Error(), "invalid file") {
		t.Errorf("err = %v", err)
	}
}

func TestProcessVideo_HLSLadder(t *testing.T) {
	requireFFmpeg(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "in.mp4")
	// 3 s synthetic clip at 640x360 with a tone.
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=size=640x360:rate=24:duration=3",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=3", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac",
		"-shortest", in).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	out := filepath.Join(dir, "out")
	_ = os.Mkdir(out, 0o755)
	proc := &Processor{}
	res, err := proc.Video(context.Background(), in, out)
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 640 || res.Height != 360 || res.DurationMS < 2900 || res.Variants.HLS != "master.m3u8" {
		t.Fatalf("result = %+v", res)
	}
	// 640x360 source: no upscaled rungs.
	if strings.Join(res.Variants.Renditions, ",") != "240p,360p" {
		t.Errorf("renditions = %v", res.Variants.Renditions)
	}
	if res.Variants.Poster == nil || res.Variants.Poster.Width != 640 || res.Placeholder == "" {
		t.Errorf("poster = %+v", res.Variants.Poster)
	}
	types := map[string]int{}
	for _, f := range res.Files {
		types[f.ContentType]++
	}
	if types["application/vnd.apple.mpegurl"] != 3 || types["video/mp2t"] < 2 || types["image/jpeg"] != 1 ||
		types["image/webp"] != perVariant(proc)-1 {
		t.Errorf("files by type = %v", types)
	}
	master, _ := os.ReadFile(filepath.Join(out, "master.m3u8"))
	if !strings.Contains(string(master), "360p/playlist.m3u8") {
		t.Errorf("master = %s", master)
	}
}
