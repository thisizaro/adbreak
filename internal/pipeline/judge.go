package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/thisizaro/adbreak/internal/ai"
	"github.com/thisizaro/adbreak/internal/breaks"
	"github.com/thisizaro/adbreak/internal/media"
	"github.com/thisizaro/adbreak/internal/speech"
)

const RejectAIUtterance = "AI heard speech across the cut"

type judgement struct {
	Index        int     `json:"index"`
	MidUtterance string  `json:"mid_utterance"`
	NaturalBreak float64 `json:"natural_break"`
	SceneChange  string  `json:"scene_change"`
	Rationale    string  `json:"rationale"`
}

var judgeSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"candidates": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"index":         map[string]any{"type": "integer"},
					"mid_utterance": map[string]any{"type": "string", "enum": []string{"yes", "no", "unsure"}},
					"natural_break": map[string]any{"type": "number"},
					"scene_change":  map[string]any{"type": "string", "enum": []string{"new_scene", "same_scene"}},
					"rationale":     map[string]any{"type": "string"},
				},
				"required": []string{"index", "mid_utterance", "natural_break", "scene_change", "rationale"},
			},
		},
	},
	"required": []string{"candidates"},
}

const judgePrompt = `You are reviewing candidate ad-break cut points in a Bengali TV drama.
For each candidate you get: a 10 second audio clip centred on the cut (the cut is at exactly 5.0s into the clip),
one frame from 1s before the cut and one frame from 1s after, and the transcript nearby (may be imperfect).

For each candidate answer:
- mid_utterance: "yes" if any person is speaking across or within 0.5s of the 5.0s mark, or a sentence is clearly unfinished at the cut; "unsure" if you cannot tell; "no" only if speech has clearly ended and the cut falls in a pause, music or silence.
- natural_break: 0 to 1, how natural and non-jarring it is to interrupt the story here for an ad. High for the end of a scene or a narrative beat, a location or time change, a pause after a line lands. Low for mid-conversation, mid-action, or right before a line is answered.
- scene_change: "new_scene" if the frame after is a different scene (place, time or situation) from the frame before, else "same_scene".
- rationale: one short sentence.
Return one entry per candidate, using the given index.`

// Judge runs the AI speech and break-quality check on candidates that passed
// the hard filters, in batches. Candidates the model flags as mid-utterance
// (or unsure) are rejected; the rest get the model's score.
func (d Deps) Judge(ctx context.Context, ep Episode, cands []breaks.Candidate, tr speech.Transcript) ([]breaks.Candidate, error) {
	return cached(ep.Dir, "judged.json", func() ([]breaks.Candidate, error) {
		work := filepath.Join(ep.Dir, "judge")
		if err := os.MkdirAll(work, 0o755); err != nil {
			return nil, err
		}
		var live []int
		for i, c := range cands {
			if c.Rejected == "" {
				live = append(live, i)
			}
		}
		out := append([]breaks.Candidate(nil), cands...)
		const batch = 6
		for s := 0; s < len(live); s += batch {
			idx := live[s:min(s+batch, len(live))]
			parts := []ai.Part{ai.Text(judgePrompt)}
			for _, i := range idx {
				t := cands[i].T
				clip := filepath.Join(work, fmt.Sprintf("c%d.mp3", i))
				before := filepath.Join(work, fmt.Sprintf("c%d_before.jpg", i))
				after := filepath.Join(work, fmt.Sprintf("c%d_after.jpg", i))
				if err := media.AudioClip(ctx, ep.Video, t-5, 10, clip); err != nil {
					return nil, err
				}
				if err := media.Frame(ctx, ep.Video, t-1, 320, before); err != nil {
					return nil, err
				}
				if err := media.Frame(ctx, ep.Video, t+1, 320, after); err != nil {
					return nil, err
				}
				parts = append(parts, ai.Text(fmt.Sprintf("Candidate index %d at %.2fs. Transcript nearby: %q", i, t, tr.TextBetween(t-8, t+8))))
				for _, f := range []struct{ path, mime string }{{clip, "audio/mp3"}, {before, "image/jpeg"}, {after, "image/jpeg"}} {
					b, err := os.ReadFile(f.path)
					if err != nil {
						return nil, err
					}
					parts = append(parts, ai.Blob(f.mime, b))
				}
			}
			var resp struct {
				Candidates []judgement `json:"candidates"`
			}
			if err := d.AI.JSON(ctx, parts, judgeSchema, &resp); err != nil {
				return nil, err
			}
			got := map[int]judgement{}
			for _, j := range resp.Candidates {
				got[j.Index] = j
			}
			for _, i := range idx {
				j, ok := got[i]
				switch {
				case !ok:
					out[i].Rejected = "AI returned no verdict (treated as unsure)"
				case j.MidUtterance != "no":
					out[i].Rejected = RejectAIUtterance + " (" + j.MidUtterance + ")"
					out[i].Rationale = j.Rationale
				default:
					out[i].Score = j.NaturalBreak
					if j.SceneChange == "new_scene" {
						out[i].Signals = append(out[i].Signals, "ai:new_scene")
					}
					out[i].Rationale = j.Rationale
				}
			}
			d.progress("judge", "batch %d/%d done", s/batch+1, (len(live)+batch-1)/batch)
		}
		return out, nil
	})
}
