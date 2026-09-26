// Package brands loads the synthetic catalogue at runtime and turns model
// verdicts into a placement. Negative contexts are enforced here, in code: a
// brand is blocked if any of its negative contexts is present or uncertain in
// the scene before or after the break, or if the model failed to answer.
package brands

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

type Creative struct {
	ID       string  `json:"id"`
	Seconds  float64 `json:"duration_sec"`
	Language string  `json:"language"`
	URL      string  `json:"url"`
}

type Brand struct {
	ID        string     `json:"brand_id"`
	Name      string     `json:"display_name"`
	Category  string     `json:"category"`
	Target    []string   `json:"target_contexts"`
	Negative  []string   `json:"negative_contexts"`
	Creatives []Creative `json:"creatives"`
}

func Load(path string) ([]Brand, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Brand
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("brands %s: %w", path, err)
	}
	for _, br := range out {
		if br.ID == "" || len(br.Creatives) == 0 {
			return nil, fmt.Errorf("brands %s: brand %q needs brand_id and creatives", path, br.ID)
		}
	}
	return out, nil
}

type Answer string

const (
	Yes    Answer = "yes"
	No     Answer = "no"
	Unsure Answer = "unsure"
)

type NegCheck struct {
	Context  string `json:"context"`
	Before   Answer `json:"before"`
	After    Answer `json:"after"`
	Evidence string `json:"evidence"`
}

type Verdict struct {
	BrandID   string     `json:"brand_id"`
	Fit       float64    `json:"fit"`
	Rationale string     `json:"rationale"`
	Negatives []NegCheck `json:"negatives"`
}

// MinFit is the floor below which a clean brand is still not worth the slot.
const MinFit = 0.2

type Decision struct {
	BrandID    string            `json:"brand_id,omitempty"`
	CreativeID string            `json:"creative_id,omitempty"`
	Seconds    float64           `json:"seconds,omitempty"`
	Fit        float64           `json:"fit,omitempty"`
	Rationale  string            `json:"rationale,omitempty"`
	Blocked    map[string]string `json:"blocked"`
	Unblocked  []string          `json:"unblocked"`
}

// Flags are negative contexts raised by signals independent of the placement
// model (a second model's safety check, scene tags). Keyed by context; the
// value is the evidence. Any flag blocks every brand that lists that context.
type Flags map[string]string

func Decide(catalogue []Brand, verdicts []Verdict, flags Flags, podSeconds float64) Decision {
	byID := map[string]Verdict{}
	for _, v := range verdicts {
		byID[v.BrandID] = v
	}
	d := Decision{Blocked: map[string]string{}, Unblocked: []string{}}
	type cand struct {
		b Brand
		v Verdict
	}
	var ok []cand
	for _, b := range catalogue {
		v, found := byID[b.ID]
		if !found {
			d.Blocked[b.ID] = "no verdict from model (treated as unsure)"
			continue
		}
		if reason := flagReason(b, flags); reason != "" {
			d.Blocked[b.ID] = reason
			continue
		}
		if reason := blockReason(b, v); reason != "" {
			d.Blocked[b.ID] = reason
			continue
		}
		d.Unblocked = append(d.Unblocked, b.ID)
		if v.Fit >= MinFit {
			ok = append(ok, cand{b, v})
		}
	}
	sort.SliceStable(ok, func(i, j int) bool {
		if ok[i].v.Fit != ok[j].v.Fit {
			return ok[i].v.Fit > ok[j].v.Fit
		}
		return ok[i].b.ID < ok[j].b.ID
	})
	for _, c := range ok {
		if cr, found := pickCreative(c.b.Creatives, podSeconds); found {
			d.BrandID, d.CreativeID, d.Seconds, d.Fit, d.Rationale = c.b.ID, cr.ID, cr.Seconds, c.v.Fit, c.v.Rationale
			break
		}
	}
	return d
}

func flagReason(b Brand, flags Flags) string {
	for _, neg := range b.Negative {
		if ev, ok := flags[neg]; ok {
			return fmt.Sprintf("%q flagged by independent check: %s", neg, ev)
		}
	}
	return ""
}

func blockReason(b Brand, v Verdict) string {
	checks := map[string]NegCheck{}
	for _, n := range v.Negatives {
		checks[n.Context] = n
	}
	for _, neg := range b.Negative {
		n, found := checks[neg]
		switch {
		case !found:
			return fmt.Sprintf("%q not assessed (treated as unsure)", neg)
		case n.Before != No:
			return fmt.Sprintf("%q %s in scene before: %s", neg, n.Before, n.Evidence)
		case n.After != No:
			return fmt.Sprintf("%q %s in scene after: %s", neg, n.After, n.Evidence)
		}
	}
	return ""
}

func pickCreative(cs []Creative, pod float64) (Creative, bool) {
	var best Creative
	found := false
	for _, c := range cs {
		if c.Seconds <= pod && (!found || c.Seconds > best.Seconds) {
			best, found = c, true
		}
	}
	return best, found
}

var safeBrandID = regexp.MustCompile(`^[a-z0-9_]{2,40}$`)

// Validate checks a brand supplied at runtime before it can reach the pipeline.
func (b Brand) Validate() error {
	switch {
	case !safeBrandID.MatchString(b.ID):
		return fmt.Errorf("brand_id must be 2-40 chars of a-z, 0-9, _")
	case strings.TrimSpace(b.Name) == "" || len(b.Name) > 60:
		return fmt.Errorf("display_name is required (max 60 chars)")
	case len(b.Target) == 0 || len(b.Target) > 30:
		return fmt.Errorf("target_contexts needs 1-30 entries")
	case len(b.Negative) > 30:
		return fmt.Errorf("negative_contexts allows at most 30 entries")
	case len(b.Creatives) == 0 || len(b.Creatives) > 5:
		return fmt.Errorf("creatives needs 1-5 entries")
	}
	for _, c := range b.Creatives {
		if !safeBrandID.MatchString(c.ID) || c.Seconds < 5 || c.Seconds > 60 {
			return fmt.Errorf("creative %q needs an id of a-z, 0-9, _ and duration_sec 5-60", c.ID)
		}
	}
	for _, s := range append(append([]string{}, b.Target...), b.Negative...) {
		if strings.TrimSpace(s) == "" || len(s) > 40 {
			return fmt.Errorf("contexts must be non-empty and at most 40 chars")
		}
	}
	return nil
}
