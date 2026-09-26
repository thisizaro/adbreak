// Package media wraps ffprobe and ffmpeg: probing, shot detection, audio
// extraction and frame grabs. It holds no AI logic.
package media

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Info struct {
	Duration float64 `json:"duration"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	FPS      float64 `json:"fps"`
	HasAudio bool    `json:"has_audio"`
}

func Probe(ctx context.Context, path string) (Info, error) {
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", path).Output()
	if err != nil {
		return Info{}, fmt.Errorf("ffprobe %s: %w", path, err)
	}
	var raw struct {
		Format  struct{ Duration string } `json:"format"`
		Streams []struct {
			CodecType  string `json:"codec_type"`
			Width      int    `json:"width"`
			Height     int    `json:"height"`
			RFrameRate string `json:"r_frame_rate"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return Info{}, fmt.Errorf("ffprobe json: %w", err)
	}
	var info Info
	info.Duration, _ = strconv.ParseFloat(raw.Format.Duration, 64)
	for _, s := range raw.Streams {
		switch s.CodecType {
		case "video":
			info.Width, info.Height = s.Width, s.Height
			if n, d, ok := strings.Cut(s.RFrameRate, "/"); ok {
				num, _ := strconv.ParseFloat(n, 64)
				den, _ := strconv.ParseFloat(d, 64)
				if den > 0 {
					info.FPS = math.Round(num/den*100) / 100
				}
			}
		case "audio":
			info.HasAudio = true
		}
	}
	if info.Duration <= 0 {
		return info, fmt.Errorf("ffprobe %s: no duration", path)
	}
	return info, nil
}

var ptsTime = regexp.MustCompile(`pts_time:([0-9.]+)`)

// DetectShots returns hard-cut timestamps in seconds using ffmpeg's scene
// score. Frames are downscaled first; cut detection does not need full resolution.
func DetectShots(ctx context.Context, path string, threshold float64) ([]float64, error) {
	filter := fmt.Sprintf("scale=160:-2,select='gt(scene,%g)',showinfo", threshold)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-nostats", "-i", path,
		"-an", "-vf", filter, "-f", "null", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg shots: %w: %s", err, tail(stderr.String()))
	}
	var cuts []float64
	sc := bufio.NewScanner(&stderr)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.Contains(line, "Parsed_showinfo") {
			continue
		}
		if m := ptsTime.FindStringSubmatch(line); m != nil {
			if t, err := strconv.ParseFloat(m[1], 64); err == nil {
				cuts = append(cuts, t)
			}
		}
	}
	return cuts, nil
}

type AudioChunk struct {
	Path   string  `json:"path"`
	Offset float64 `json:"offset"`
	Length float64 `json:"length"`
}

// ExtractAudioChunks writes 16kHz mono MP3 chunks of chunkLen seconds, each
// overlapping the next by overlap seconds, so every chunk stays under ASR upload caps.
func ExtractAudioChunks(ctx context.Context, path, dir string, chunkLen, overlap float64) ([]AudioChunk, error) {
	info, err := Probe(ctx, path)
	if err != nil {
		return nil, err
	}
	step := chunkLen - overlap
	if step <= 0 {
		return nil, fmt.Errorf("overlap %g must be smaller than chunk %g", overlap, chunkLen)
	}
	var chunks []AudioChunk
	for i, start := 0, 0.0; start < info.Duration-0.05; i, start = i+1, start+step {
		length := math.Min(chunkLen, info.Duration-start)
		out := filepath.Join(dir, fmt.Sprintf("chunk_%03d.mp3", i))
		cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-y",
			"-ss", fmt.Sprintf("%.3f", start), "-t", fmt.Sprintf("%.3f", length), "-i", path,
			"-vn", "-ac", "1", "-ar", "16000", "-b:a", "32k", out)
		if b, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("ffmpeg audio chunk %d: %w: %s", i, err, tail(string(b)))
		}
		chunks = append(chunks, AudioChunk{Path: out, Offset: start, Length: length})
		if start+chunkLen >= info.Duration {
			break
		}
	}
	return chunks, nil
}

// Frame writes a single JPEG frame at t seconds, scaled to the given width.
func Frame(ctx context.Context, path string, t float64, width int, out string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-y", "-ss", fmt.Sprintf("%.3f", t),
		"-i", path, "-frames:v", "1", "-vf", fmt.Sprintf("scale=%d:-2", width), "-q:v", "5", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg frame at %.2f: %w: %s", t, err, tail(string(b)))
	}
	return nil
}

func tail(s string) string {
	if len(s) > 400 {
		return s[len(s)-400:]
	}
	return s
}
