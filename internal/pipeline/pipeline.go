// Package pipeline runs the analysis stages for one episode in order. Each
// stage writes a JSON artifact to the episode's work dir and is skipped when the
// artifact already exists, so a rerun only pays for stages that changed.
package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/thisizaro/adbreak/internal/ai"
	"github.com/thisizaro/adbreak/internal/media"
	"github.com/thisizaro/adbreak/internal/speech"
)

type Deps struct {
	ASR            speech.Provider
	AI             ai.Provider
	ShotThreshold  float64
	ChunkSeconds   float64
	OverlapSeconds float64
	ASRParallelism int
	Progress       func(stage, msg string)
}

type Episode struct {
	ID    string
	Video string
	Dir   string
}

func (d Deps) progress(stage, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[%s] %s", stage, msg)
	if d.Progress != nil {
		d.Progress(stage, msg)
	}
}

// cached loads dst from dir/name if present, otherwise runs fn and stores its result.
func cached[T any](dir, name string, fn func() (T, error)) (T, error) {
	path := filepath.Join(dir, name)
	var v T
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &v); err == nil {
			return v, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return v, err
	}
	v, err := fn()
	if err != nil {
		return v, err
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	return v, os.WriteFile(path, b, 0o644)
}

type Media struct {
	Info  media.Info `json:"info"`
	Shots []float64  `json:"shots"`
}

func (d Deps) Media(ctx context.Context, ep Episode) (Media, error) {
	return cached(ep.Dir, "media.json", func() (Media, error) {
		start := time.Now()
		info, err := media.Probe(ctx, ep.Video)
		if err != nil {
			return Media{}, err
		}
		shots, err := media.DetectShots(ctx, ep.Video, d.ShotThreshold)
		if err != nil {
			return Media{}, err
		}
		d.progress("media", "duration=%.1fs cuts=%d in %s", info.Duration, len(shots), time.Since(start).Round(time.Millisecond))
		return Media{Info: info, Shots: shots}, nil
	})
}

func (d Deps) Transcript(ctx context.Context, ep Episode) (speech.Transcript, error) {
	return cached(ep.Dir, "transcript.json", func() (speech.Transcript, error) {
		start := time.Now()
		audioDir := filepath.Join(ep.Dir, "audio")
		if err := os.MkdirAll(audioDir, 0o755); err != nil {
			return speech.Transcript{}, err
		}
		chunks, err := media.ExtractAudioChunks(ctx, ep.Video, audioDir, d.ChunkSeconds, d.OverlapSeconds)
		if err != nil {
			return speech.Transcript{}, err
		}
		parts := make([]speech.Part, len(chunks))
		errs := make([]error, len(chunks))
		sem := make(chan struct{}, max(1, d.ASRParallelism))
		var wg sync.WaitGroup
		for i, c := range chunks {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				tr, err := d.ASR.Transcribe(ctx, c.Path)
				parts[i], errs[i] = speech.Part{Offset: c.Offset, Length: c.Length, T: tr}, err
			}()
		}
		wg.Wait()
		if err := errors.Join(errs...); err != nil {
			return speech.Transcript{}, err
		}
		tr := speech.Merge(parts)
		d.progress("speech", "%s chunks=%d segments=%d words=%d in %s", d.ASR.Name(), len(chunks), len(tr.Segments), len(tr.Words), time.Since(start).Round(time.Millisecond))
		return tr, nil
	})
}
