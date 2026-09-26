package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/thisizaro/adbreak/internal/ai"
	"github.com/thisizaro/adbreak/internal/media"
	"github.com/thisizaro/adbreak/internal/scenes"
	"github.com/thisizaro/adbreak/internal/speech"
)

const (
	sceneWindow        = 80
	histogramThreshold = 0.45
)

var scenesSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"first_scene_continues_previous": map[string]any{"type": "boolean"},
		"scenes": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"first_shot":        map[string]any{"type": "integer"},
					"description":       map[string]any{"type": "string"},
					"dominant_activity": map[string]any{"type": "string"},
					"contexts":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"mood":              map[string]any{"type": "string"},
				},
				"required": []string{"first_shot", "description", "dominant_activity", "contexts", "mood"},
			},
		},
	},
	"required": []string{"first_scene_continues_previous", "scenes"},
}

const scenesPrompt = `You are segmenting a Bengali TV drama into scenes. A scene is a continuous stretch of story in one place and time
with the same situation; a conversation filmed as many shot-reverse-shot angles is ONE scene. A new scene starts when the
place, the time, or the situation clearly changes.

You get consecutive shots, each with its index, time span and one keyframe, plus the transcript for this stretch.
Shots marked [proposed] are where a colour change and a pause suggest a new scene; treat these as hints only.

Return the scenes in order. Each scene gives first_shot (the index of the shot where it starts), a one-sentence
description, the dominant activity, short context tags (place, activity, event, e.g. kitchen, eating, funeral, car, office),
and the mood. The first scene must start at the first shot you were given. Set first_scene_continues_previous to true
if the first shot here clearly continues the scene from just before this stretch.`

func (d Deps) Scenes(ctx context.Context, ep Episode, m Media, tr speech.Transcript) ([]scenes.Scene, error) {
	return cached(ep.Dir, "scenes.json", func() ([]scenes.Scene, error) {
		shots := scenes.Shots(m.Shots, m.Info.Duration)
		dir := filepath.Join(ep.Dir, "keyframes")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		hists := make([][]float64, len(shots))
		errs := make([]error, len(shots))
		sem := make(chan struct{}, 8)
		var wg sync.WaitGroup
		for i := range shots {
			shots[i].Frame = filepath.Join(dir, fmt.Sprintf("s%04d.jpg", i))
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				mid := (shots[i].Start + shots[i].End) / 2
				if err := media.Frame(ctx, ep.Video, mid, 192, shots[i].Frame); err != nil {
					errs[i] = err
					return
				}
				hists[i], errs[i] = scenes.Histogram(shots[i].Frame)
			}()
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				return nil, err
			}
		}
		speechAt := make([]bool, len(shots))
		for i := 1; i < len(shots); i++ {
			speechAt[i] = tr.SpeechNear(shots[i].Start, 0.3)
		}
		proposed := scenes.Propose(hists, speechAt, histogramThreshold)
		nProp := 0
		for _, p := range proposed {
			if p {
				nProp++
			}
		}
		d.progress("scenes", "%d shots, %d heuristic scene starts", len(shots), nProp)

		starts := make([]bool, len(shots))
		type meta struct {
			desc, act, mood string
			ctx             []string
		}
		metas := map[int]meta{}
		aiWindows, fallback := 0, 0
		for w := 0; w < len(shots); w += sceneWindow {
			hi := min(w+sceneWindow, len(shots))
			parts := []ai.Part{ai.Text(scenesPrompt),
				ai.Text(fmt.Sprintf("Transcript %.0fs to %.0fs: %q", shots[w].Start, shots[hi-1].End, tr.TextBetween(shots[w].Start, shots[hi-1].End)))}
			for i := w; i < hi; i++ {
				b, err := os.ReadFile(shots[i].Frame)
				if err != nil {
					return nil, err
				}
				hint := ""
				if proposed[i] {
					hint = " [proposed]"
				}
				parts = append(parts, ai.Text(fmt.Sprintf("shot %d (%.1fs to %.1fs)%s", i, shots[i].Start, shots[i].End, hint)), ai.Blob("image/jpeg", b))
			}
			var resp struct {
				Continues bool `json:"first_scene_continues_previous"`
				Scenes    []struct {
					FirstShot   int      `json:"first_shot"`
					Description string   `json:"description"`
					Activity    string   `json:"dominant_activity"`
					Contexts    []string `json:"contexts"`
					Mood        string   `json:"mood"`
				} `json:"scenes"`
			}
			if err := d.AI.JSON(ctx, parts, scenesSchema, &resp); err != nil {
				d.progress("scenes", "window %d AI failed, using heuristic: %v", w/sceneWindow, err)
				for i := w; i < hi; i++ {
					starts[i] = proposed[i]
				}
				fallback++
				continue
			}
			aiWindows++
			sort.Slice(resp.Scenes, func(a, b int) bool { return resp.Scenes[a].FirstShot < resp.Scenes[b].FirstShot })
			for k, s := range resp.Scenes {
				first := s.FirstShot
				if k == 0 {
					first = w // the model is told the first scene starts here
				}
				if first < w || first >= hi {
					continue
				}
				starts[first] = true
				metas[first] = meta{s.Description, s.Activity, s.Mood, s.Contexts}
			}
			if resp.Continues && w > 0 {
				starts[w] = false
			}
		}
		source := "ai"
		if aiWindows == 0 {
			source = "heuristic"
		}
		out := scenes.Build(shots, starts, source)
		for i := range out {
			if m, ok := metas[out[i].FirstShot]; ok {
				out[i].Description, out[i].Activity, out[i].Mood, out[i].Contexts = m.desc, m.act, m.mood, m.ctx
			}
		}
		d.progress("scenes", "%d scenes (%d AI windows, %d heuristic fallbacks)", len(out), aiWindows, fallback)
		return out, nil
	})
}
