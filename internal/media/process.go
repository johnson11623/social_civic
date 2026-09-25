package media

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Processing with ffmpeg for images and video alike (docs/media §6): one
// pinned tool gives production-equivalent variants locally.

// ImageSizes are the image variants (docs/media §6, image handler).
var ImageSizes = []struct {
	Name  string
	Width int
}{{"thumbnail", 320}, {"medium", 1080}, {"large", 2048}}

// Rendition is one rung of the HLS ladder.
type Rendition struct {
	Name          string
	Width, Height int
	VideoKbps     int
	AudioKbps     int
}

// Ladder is the full HLS ladder (docs/media §6, video handler).
var Ladder = []Rendition{
	{"240p", 426, 240, 300, 64},
	{"360p", 640, 360, 600, 96},
	{"480p", 854, 480, 1200, 128},
	{"720p", 1280, 720, 2500, 128},
	{"1080p", 1920, 1080, 4500, 192},
}

// Limits on what the worker accepts from a probe.
const (
	maxVideoDuration = 10 * 60 * 1000 // ms
	maxPixels        = 50_000_000     // decompression-bomb guard
)

// ErrInvalidMedia marks a file that can't be processed (not a retryable fault).
var ErrInvalidMedia = errors.New("media: invalid file")

// Processor runs ffmpeg/ffprobe.
type Processor struct {
	FFmpeg     string      // default "ffmpeg"
	FFprobe    string      // default "ffprobe"
	Renditions []Rendition // default Ladder; a shorter list speeds up local work

	webpOnce sync.Once
	webp     bool
}

// hasWebP reports whether this ffmpeg can encode WebP (the container's can;
// e.g. Homebrew's can't). Without it variants are JPEG only.
func (p *Processor) hasWebP(ctx context.Context) bool {
	p.webpOnce.Do(func() {
		out, err := p.run(ctx, "ffmpeg", "-hide_banner", "-encoders")
		p.webp = err == nil && bytes.Contains(out, []byte("libwebp"))
	})
	return p.webp
}

// OutFile is a produced file to upload, keyed relative to the media's prefix.
type OutFile struct {
	Path        string
	Key         string
	ContentType string
}

// Result is what processing produced.
type Result struct {
	Width, Height, DurationMS int
	Placeholder               string // data: URI
	Variants                  Variants
	Files                     []OutFile
}

type probe struct {
	Width, Height int
	DurationMS    int
	HasAudio      bool
}

func (p *Processor) bin(name string) string {
	switch {
	case name == "ffmpeg" && p.FFmpeg != "":
		return p.FFmpeg
	case name == "ffprobe" && p.FFprobe != "":
		return p.FFprobe
	}
	return name
}

func (p *Processor) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, p.bin(name), args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 500 {
			msg = msg[len(msg)-500:]
		}
		return nil, fmt.Errorf("%s: %w: %s", name, err, msg)
	}
	return stdout.Bytes(), nil
}

func (p *Processor) probe(ctx context.Context, in string) (probe, error) {
	out, err := p.run(ctx, "ffprobe", "-v", "error", "-show_entries", "stream=codec_type,width,height:format=duration",
		"-of", "json", in)
	if err != nil {
		return probe{}, fmt.Errorf("%w: %v", ErrInvalidMedia, err)
	}
	var doc struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return probe{}, fmt.Errorf("%w: probe output: %v", ErrInvalidMedia, err)
	}
	var pr probe
	for _, s := range doc.Streams {
		switch s.CodecType {
		case "video":
			if pr.Width == 0 {
				pr.Width, pr.Height = s.Width, s.Height
			}
		case "audio":
			pr.HasAudio = true
		}
	}
	if d, err := strconv.ParseFloat(doc.Format.Duration, 64); err == nil {
		pr.DurationMS = int(d * 1000)
	}
	if pr.Width <= 0 || pr.Height <= 0 {
		return probe{}, fmt.Errorf("%w: no picture", ErrInvalidMedia)
	}
	if pr.Width*pr.Height > maxPixels {
		return probe{}, fmt.Errorf("%w: %dx%d is too many pixels", ErrInvalidMedia, pr.Width, pr.Height)
	}
	return pr, nil
}

// scaledHeight keeps the aspect ratio at width w, rounded to even (codecs need it).
func scaledHeight(srcW, srcH, w int) int {
	h := srcH * w / srcW
	if h%2 == 1 {
		h++
	}
	return max(h, 2)
}

// capWidth never enlarges.
func capWidth(src, want int) int { return min(src, want) }

// imageVariant writes name.webp and name.jpg at width w with no metadata:
// re-encoding drops EXIF and GPS (docs/media: EXIF stripping), and
// -map_metadata -1 drops container tags too.
func (p *Processor) imageVariant(ctx context.Context, in, dir, name string, srcW, srcH, w int) (ImageVariant, []OutFile, error) {
	h := scaledHeight(srcW, srcH, w)
	scale := fmt.Sprintf("scale=%d:%d:flags=lanczos", w, h)
	jpg := filepath.Join(dir, name+".jpg")
	if _, err := p.run(ctx, "ffmpeg", "-y", "-v", "error", "-i", in, "-map_metadata", "-1", "-frames:v", "1",
		"-vf", scale, "-q:v", "3", jpg); err != nil {
		return ImageVariant{}, nil, err
	}
	v := ImageVariant{Name: name, Width: w, Height: h, JPEG: name + ".jpg"}
	files := []OutFile{{jpg, v.JPEG, "image/jpeg"}}
	if p.hasWebP(ctx) {
		webp := filepath.Join(dir, name+".webp")
		if _, err := p.run(ctx, "ffmpeg", "-y", "-v", "error", "-i", in, "-map_metadata", "-1", "-frames:v", "1",
			"-vf", scale, "-c:v", "libwebp", "-quality", "85", webp); err != nil {
			return ImageVariant{}, nil, err
		}
		v.WebP = name + ".webp"
		files = append(files, OutFile{webp, v.WebP, "image/webp"})
	}
	return v, files, nil
}

// placeholder is a 16-pixel-wide blurred WebP as a data: URI (a few hundred
// bytes) that pages show while the real image loads.
func (p *Processor) placeholder(ctx context.Context, in, dir string) (string, error) {
	args, mime, out := []string{"-c:v", "libwebp", "-quality", "40"}, "image/webp", filepath.Join(dir, "placeholder.webp")
	if !p.hasWebP(ctx) {
		args, mime, out = []string{"-q:v", "8"}, "image/jpeg", filepath.Join(dir, "placeholder.jpg")
	}
	cmd := append([]string{"-y", "-v", "error", "-i", in, "-map_metadata", "-1", "-frames:v", "1",
		"-vf", "scale=16:-2,gblur=sigma=1"}, args...)
	if _, err := p.run(ctx, "ffmpeg", append(cmd, out)...); err != nil {
		return "", err
	}
	b, err := os.ReadFile(out)
	if err != nil {
		return "", err
	}
	_ = os.Remove(out)
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b), nil
}

// Image produces the thumbnail/medium/large variants and a placeholder.
func (p *Processor) Image(ctx context.Context, in, dir string) (Result, error) {
	pr, err := p.probe(ctx, in)
	if err != nil {
		return Result{}, err
	}
	res := Result{Width: pr.Width, Height: pr.Height}
	for _, size := range ImageSizes {
		v, files, err := p.imageVariant(ctx, in, dir, size.Name, pr.Width, pr.Height, capWidth(pr.Width, size.Width))
		if err != nil {
			return Result{}, err
		}
		res.Variants.Images = append(res.Variants.Images, v)
		res.Files = append(res.Files, files...)
	}
	if res.Placeholder, err = p.placeholder(ctx, in, dir); err != nil {
		return Result{}, err
	}
	return res, nil
}

// renditionsFor keeps the rungs at or below the source height (never
// upscale), always at least the lowest one.
func renditionsFor(ladder []Rendition, srcH int) []Rendition {
	var out []Rendition
	for _, r := range ladder {
		if r.Height <= srcH {
			out = append(out, r)
		}
	}
	if len(out) == 0 && len(ladder) > 0 {
		out = ladder[:1]
	}
	return out
}

// masterPlaylist lists the renditions for adaptive playback.
func masterPlaylist(rs []Rendition, srcW, srcH int) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n")
	for _, r := range rs {
		h := min(r.Height, srcH)
		w := scaledHeight(srcH, srcW, h) // width for this height, even
		fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d\n%s/playlist.m3u8\n",
			(r.VideoKbps+r.AudioKbps)*1000, w, h, r.Name)
	}
	return b.String()
}

// Video produces a poster frame (as image variants) and the HLS ladder.
func (p *Processor) Video(ctx context.Context, in, dir string) (Result, error) {
	pr, err := p.probe(ctx, in)
	if err != nil {
		return Result{}, err
	}
	if pr.DurationMS <= 0 || pr.DurationMS > maxVideoDuration {
		return Result{}, fmt.Errorf("%w: duration %d ms", ErrInvalidMedia, pr.DurationMS)
	}
	res := Result{Width: pr.Width, Height: pr.Height, DurationMS: pr.DurationMS}

	poster := filepath.Join(dir, "poster-src.jpg")
	if _, err := p.run(ctx, "ffmpeg", "-y", "-v", "error", "-ss", "0", "-i", in, "-frames:v", "1", "-q:v", "2", poster); err != nil {
		return Result{}, err
	}
	pv, files, err := p.imageVariant(ctx, poster, dir, "poster", pr.Width, pr.Height, capWidth(pr.Width, 1280))
	if err != nil {
		return Result{}, err
	}
	res.Variants.Poster, res.Files = &pv, append(res.Files, files...)
	if res.Placeholder, err = p.placeholder(ctx, poster, dir); err != nil {
		return Result{}, err
	}
	_ = os.Remove(poster)

	ladder := p.Renditions
	if len(ladder) == 0 {
		ladder = Ladder
	}
	rs := renditionsFor(ladder, pr.Height)
	for _, r := range rs {
		out := filepath.Join(dir, r.Name)
		if err := os.MkdirAll(out, 0o755); err != nil {
			return Result{}, err
		}
		args := []string{"-y", "-v", "error", "-i", in, "-map_metadata", "-1",
			"-vf", fmt.Sprintf("scale=-2:%d", min(r.Height, pr.Height)),
			"-c:v", "libx264", "-preset", "veryfast", "-profile:v", "main", "-b:v", fmt.Sprintf("%dk", r.VideoKbps),
			"-maxrate", fmt.Sprintf("%dk", r.VideoKbps*3/2), "-bufsize", fmt.Sprintf("%dk", r.VideoKbps*2),
			"-g", "48", "-keyint_min", "48", "-sc_threshold", "0"}
		if pr.HasAudio {
			args = append(args, "-c:a", "aac", "-b:a", fmt.Sprintf("%dk", r.AudioKbps), "-ac", "2")
		} else {
			args = append(args, "-an")
		}
		args = append(args, "-f", "hls", "-hls_time", "6", "-hls_playlist_type", "vod",
			"-hls_segment_filename", filepath.Join(out, "segment%03d.ts"), filepath.Join(out, "playlist.m3u8"))
		if _, err := p.run(ctx, "ffmpeg", args...); err != nil {
			return Result{}, err
		}
		res.Variants.Renditions = append(res.Variants.Renditions, r.Name)
	}
	master := filepath.Join(dir, "master.m3u8")
	if err := os.WriteFile(master, []byte(masterPlaylist(rs, pr.Width, pr.Height)), 0o644); err != nil {
		return Result{}, err
	}
	res.Variants.HLS = "master.m3u8"
	// Every playlist and segment under the renditions.
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		switch filepath.Ext(path) {
		case ".m3u8":
			res.Files = append(res.Files, OutFile{path, filepath.ToSlash(rel), "application/vnd.apple.mpegurl"})
		case ".ts":
			res.Files = append(res.Files, OutFile{path, filepath.ToSlash(rel), "video/mp2t"})
		}
		return nil
	})
	return res, err
}

// ParseRenditions picks ladder rungs by name ("240p,480p"); empty means all.
func ParseRenditions(list string) ([]Rendition, error) {
	if strings.TrimSpace(list) == "" {
		return Ladder, nil
	}
	var out []Rendition
	for _, name := range strings.Split(list, ",") {
		name = strings.TrimSpace(name)
		found := false
		for _, r := range Ladder {
			if r.Name == name {
				out, found = append(out, r), true
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown rendition %q (have 240p, 360p, 480p, 720p, 1080p)", name)
		}
	}
	return out, nil
}
