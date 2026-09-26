package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/thisizaro/adbreak/internal/ai"
	"github.com/thisizaro/adbreak/internal/brands"
	"github.com/thisizaro/adbreak/internal/media"
	"github.com/thisizaro/adbreak/internal/speech"
)

type SceneRead struct {
	Description string   `json:"description"`
	Activity    string   `json:"dominant_activity"`
	Contexts    []string `json:"contexts"`
}

type Placement struct {
	T        float64          `json:"t"`
	Before   SceneRead        `json:"scene_before"`
	After    SceneRead        `json:"scene_after"`
	Verdicts []brands.Verdict `json:"verdicts"`
	Decision brands.Decision  `json:"decision"`
}

var sceneSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"description":       map[string]any{"type": "string"},
		"dominant_activity": map[string]any{"type": "string"},
		"contexts":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	},
	"required": []string{"description", "dominant_activity", "contexts"},
}

var placeSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"scene_before": sceneSchema,
		"scene_after":  sceneSchema,
		"verdicts": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"brand_id":  map[string]any{"type": "string"},
					"fit":       map[string]any{"type": "number"},
					"rationale": map[string]any{"type": "string"},
					"negatives": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"context":  map[string]any{"type": "string"},
								"before":   map[string]any{"type": "string", "enum": []string{"yes", "no", "unsure"}},
								"after":    map[string]any{"type": "string", "enum": []string{"yes", "no", "unsure"}},
								"evidence": map[string]any{"type": "string"},
							},
							"required": []string{"context", "before", "after", "evidence"},
						},
					},
				},
				"required": []string{"brand_id", "fit", "rationale", "negatives"},
			},
		},
	},
	"required": []string{"scene_before", "scene_after", "verdicts"},
}

const placePrompt = `You are placing one ad at a break in a Bengali TV drama.
You get frames from the scene BEFORE the break (the last seconds before it) and from the scene AFTER it,
the transcript around the break (may be imperfect), and a brand catalogue.

1. Describe scene_before and scene_after: a one-sentence description, the dominant activity, and a list of short context tags (places, activities, moods, events).
2. For EVERY brand in the catalogue return a verdict:
   - negatives: for EVERY one of that brand's negative_contexts, exactly as spelled, say whether it is present in the scene before and in the scene after: "yes", "no" or "unsure". Judge meaning, not words: a cremation, shraddho or mourning counts as funeral and grief; someone hurt counts as injury or accident; a meal being eaten counts as eating. Use "unsure" whenever the evidence is ambiguous. Give brief evidence.
   - fit: 0 to 1, how well the brand's target_contexts match the DOMINANT activity of the scenes around the break (weigh the scene before most).
   - rationale: one short sentence.
Do not skip any brand or any negative context.`

func (d Deps) Place(ctx context.Context, ep Episode, t float64, catalogue []brands.Brand, tr speech.Transcript, podSeconds float64) (Placement, error) {
	type brief struct {
		ID       string   `json:"brand_id"`
		Category string   `json:"category"`
		Target   []string `json:"target_contexts"`
		Negative []string `json:"negative_contexts"`
	}
	var cat []brief
	for _, b := range catalogue {
		cat = append(cat, brief{b.ID, b.Category, b.Target, b.Negative})
	}
	catJSON, _ := json.Marshal(cat)
	// The catalogue is part of the cache key, so adding a brand re-runs placement.
	key := fmt.Sprintf("place_%.2f_%x.json", t, sha256.Sum256(catJSON))
	key = key[:len("place_")+len(fmt.Sprintf("%.2f", t))+1+12] + ".json"
	return cached(ep.Dir, key, func() (Placement, error) {
		work := filepath.Join(ep.Dir, "place")
		if err := os.MkdirAll(work, 0o755); err != nil {
			return Placement{}, err
		}
		parts := []ai.Part{ai.Text(placePrompt), ai.Text("Catalogue: " + string(catJSON)),
			ai.Text(fmt.Sprintf("Transcript around the break: %q", tr.TextBetween(t-30, t+20)))}
		for _, off := range []float64{-20, -10, -4, -1, 1, 4, 10} {
			f := filepath.Join(work, fmt.Sprintf("p%.2f_%+.0f.jpg", t, off))
			if err := media.Frame(ctx, ep.Video, t+off, 320, f); err != nil {
				return Placement{}, err
			}
			b, err := os.ReadFile(f)
			if err != nil {
				return Placement{}, err
			}
			label := "BEFORE"
			if off > 0 {
				label = "AFTER"
			}
			parts = append(parts, ai.Text(fmt.Sprintf("Frame %s the break (%+.0fs):", label, off)), ai.Blob("image/jpeg", b))
		}
		model := d.Decide
		if model == nil {
			model = d.AI
		}
		var p Placement
		if err := model.JSON(ctx, parts, placeSchema, &p); err != nil {
			return Placement{}, err
		}
		p.T = t
		p.Decision = brands.Decide(catalogue, p.Verdicts, nil, podSeconds)
		d.progress("place", "t=%.1f brand=%q blocked=%d", t, p.Decision.BrandID, len(p.Decision.Blocked))
		return p, nil
	})
}
