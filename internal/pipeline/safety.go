package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/thisizaro/adbreak/internal/ai"
	"github.com/thisizaro/adbreak/internal/brands"
	"github.com/thisizaro/adbreak/internal/media"
	"github.com/thisizaro/adbreak/internal/scenes"
)

// The ad pauses content, so the viewer sees content up to t, the ad, then content
// from t on. The safety window covers what surrounds the ad in episode time.
const (
	safetyBefore = 30.0
	safetyAfter  = 40.0
)

type SafetyCheck struct {
	Context  string `json:"context"`
	Answer   string `json:"answer"`
	Evidence string `json:"evidence"`
}

var safetySchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"checks": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"context":  map[string]any{"type": "string"},
					"answer":   map[string]any{"type": "string", "enum": []string{"yes", "no", "unsure"}},
					"evidence": map[string]any{"type": "string"},
				},
				"required": []string{"context", "answer", "evidence"},
			},
		},
	},
	"required": []string{"checks"},
}

const safetyPrompt = `You are a brand-safety reviewer for ad breaks in a Bengali TV drama. The frames below span from 30 seconds
before the break to 40 seconds after it (the ad plays at the break; the story resumes right after).
For EVERY sensitive context listed, answer whether it appears anywhere in this span: "yes", "no" or "unsure".
Judge meaning, not words: cremation, shraddho or mourning count as funeral and grief; blood, a fight or a threat with a
weapon count as violence; a meal being eaten counts as eating; a patient or ward counts as hospital or illness.
Answer "unsure" whenever you cannot rule it out. Give brief evidence with the approximate offset.`

func negativeUnion(catalogue []brands.Brand) []string {
	seen := map[string]bool{}
	var out []string
	for _, b := range catalogue {
		for _, n := range b.Negative {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Safety asks the bulk model, independently of the placement model, which
// negative contexts appear around the break.
func (d Deps) Safety(ctx context.Context, ep Episode, t float64, negs []string) ([]SafetyCheck, error) {
	key, _ := json.Marshal(negs)
	name := fmt.Sprintf("safety_%.2f_%x.json", t, sha256.Sum256(key))
	name = name[:len(name)-len(".json")-52] + ".json"
	return cached(ep.Dir, name, func() ([]SafetyCheck, error) {
		work := filepath.Join(ep.Dir, "safety")
		if err := os.MkdirAll(work, 0o755); err != nil {
			return nil, err
		}
		parts := []ai.Part{ai.Text(safetyPrompt), ai.Text("Sensitive contexts: " + strings.Join(negs, ", "))}
		for _, off := range []float64{-30, -20, -10, -3, 3, 10, 20, 30, 40} {
			f := filepath.Join(work, fmt.Sprintf("s%.2f_%+.0f.jpg", t, off))
			if err := media.Frame(ctx, ep.Video, t+off, 320, f); err != nil {
				return nil, err
			}
			b, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			parts = append(parts, ai.Text(fmt.Sprintf("Frame at %+.0fs from the break:", off)), ai.Blob("image/jpeg", b))
		}
		var resp struct {
			Checks []SafetyCheck `json:"checks"`
		}
		if err := d.AI.JSON(ctx, parts, safetySchema, &resp); err != nil {
			return nil, err
		}
		d.progress("safety", "t=%.1f checked %d contexts", t, len(resp.Checks))
		return resp.Checks, nil
	})
}

// SafetyFlags merges the safety model's answers and scene-tag matches into
// flags. A context the safety model did not answer is flagged as unsure.
func SafetyFlags(negs []string, checks []SafetyCheck, sc []scenes.Scene, t float64) brands.Flags {
	flags := brands.Flags{}
	got := map[string]SafetyCheck{}
	for _, c := range checks {
		got[c.Context] = c
	}
	for _, n := range negs {
		c, ok := got[n]
		switch {
		case !ok:
			flags[n] = "safety model gave no answer (treated as unsure)"
		case c.Answer != "no":
			flags[n] = fmt.Sprintf("safety model: %s, %s", c.Answer, c.Evidence)
		}
	}
	for _, s := range sc {
		if s.End < t-safetyBefore || s.Start > t+safetyAfter {
			continue
		}
		for _, tag := range s.Contexts {
			lt := strings.ToLower(tag)
			for _, n := range negs {
				if _, done := flags[n]; done {
					continue
				}
				if strings.Contains(lt, strings.ToLower(n)) {
					flags[n] = fmt.Sprintf("scene %d tag %q (%.0fs to %.0fs)", s.Index, tag, s.Start, s.End)
				}
			}
		}
	}
	return flags
}
