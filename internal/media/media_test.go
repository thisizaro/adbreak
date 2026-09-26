package media

import (
	"context"
	"math"
	"os/exec"
	"path/filepath"
	"testing"
)

// makeClip renders red for 2s then blue for 2s with a 440Hz tone, so there is
// exactly one hard cut at t=2.0.
func makeClip(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "clip.mp4")
	cmd := exec.Command("ffmpeg", "-v", "error",
		"-f", "lavfi", "-i", "color=c=red:s=320x180:d=2:r=25",
		"-f", "lavfi", "-i", "color=c=blue:s=320x180:d=2:r=25",
		"-f", "lavfi", "-i", "sine=f=440:d=4",
		"-filter_complex", "[0:v][1:v]concat=n=2:v=1[v]",
		"-map", "[v]", "-map", "2:a", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, b)
	}
	return out
}

func TestProbe(t *testing.T) {
	info, err := Probe(context.Background(), makeClip(t))
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(info.Duration-4) > 0.2 || !info.HasAudio || info.Width != 320 || info.FPS != 25 {
		t.Fatalf("unexpected probe: %+v", info)
	}
}

func TestDetectShots(t *testing.T) {
	cuts, err := DetectShots(context.Background(), makeClip(t), 0.3)
	if err != nil {
		t.Fatal(err)
	}
	if len(cuts) != 1 || math.Abs(cuts[0]-2.0) > 0.05 {
		t.Fatalf("want one cut at 2.0, got %v", cuts)
	}
}

func TestExtractAudioChunks(t *testing.T) {
	dir := t.TempDir()
	chunks, err := ExtractAudioChunks(context.Background(), makeClip(t), dir, 1.5, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	// 4s audio, 1.5s chunks advancing by 1.0s: starts at 0, 1, 2, 3.
	if len(chunks) != 4 || chunks[1].Offset != 1.0 {
		t.Fatalf("unexpected chunks: %+v", chunks)
	}
}
