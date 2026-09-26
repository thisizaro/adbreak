// Package slates renders placeholder ad creatives for synthetic brands: a
// branded card with a countdown, at an exact duration, so the player has real
// media to cut to. The catalogue points at creatives that were not supplied.
package slates

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Spec struct {
	BrandName string
	Category  string
	Seconds   float64
	Width     int
	Height    int
	Font      string
}

var palette = []string{"0x1f4e79", "0x7b2d26", "0x2e6b30", "0x5b2c6f", "0x8a5a00", "0x145a5a", "0x6b2e4f", "0x3b3b98"}

func colour(name string) string {
	h := fnv.New32a()
	h.Write([]byte(name))
	return palette[h.Sum32()%uint32(len(palette))]
}

func esc(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `:`, `\:`, `'`, `\'`, `%`, `\%`)
	return r.Replace(s)
}

// Render writes an H.264/AAC MP4 of exactly s.Seconds to out. Existing files are kept.
func Render(ctx context.Context, s Spec, out string) error {
	if _, err := os.Stat(out); err == nil {
		return nil
	}
	// Text goes through files, not the filtergraph string, so brand names from a
	// runtime catalogue can contain any character without escaping bugs.
	dir, err := os.MkdirTemp("", "slate")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	lines := []struct {
		text, style string
	}{
		{s.BrandName, fmt.Sprintf("fontcolor=white:fontsize=%d:x=(w-tw)/2:y=h*0.36", s.Height/7)},
		{s.Category, fmt.Sprintf("fontcolor=white@0.8:fontsize=%d:x=(w-tw)/2:y=h*0.56", s.Height/20)},
		{fmt.Sprintf("AD  %%{eif:%g-t:d}s", s.Seconds), fmt.Sprintf("fontcolor=white@0.9:fontsize=%d:x=w-tw-24:y=24", s.Height/22)},
		{"Synthetic brand, generated slate", fmt.Sprintf("fontcolor=white@0.5:fontsize=%d:x=24:y=h-th-24", s.Height/30)},
	}
	var draw []string
	for i, l := range lines {
		f := filepath.Join(dir, fmt.Sprintf("t%d.txt", i))
		if err := os.WriteFile(f, []byte(l.text), 0o644); err != nil {
			return err
		}
		draw = append(draw, fmt.Sprintf("drawtext=fontfile=%s:textfile=%s:%s", esc(s.Font), esc(f), l.style))
	}
	tmp := out + ".tmp.mp4"
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-y",
		"-f", "lavfi", "-i", fmt.Sprintf("color=c=%s:s=%dx%d:r=25:d=%g", colour(s.BrandName), s.Width, s.Height, s.Seconds),
		"-f", "lavfi", "-i", fmt.Sprintf("anullsrc=r=48000:cl=stereo:d=%g", s.Seconds),
		"-vf", strings.Join(draw, ","), "-c:v", "libx264", "-preset", "veryfast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest", "-movflags", "+faststart", tmp)
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("render slate %s: %w: %s", s.BrandName, err, b)
	}
	return os.Rename(tmp, out)
}
