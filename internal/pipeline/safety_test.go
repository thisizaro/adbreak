package pipeline

import (
	"testing"

	"github.com/thisizaro/adbreak/internal/brands"
	"github.com/thisizaro/adbreak/internal/scenes"
)

func TestSafetyFlags(t *testing.T) {
	negs := []string{"eating", "funeral", "violence"}
	checks := []SafetyCheck{
		{Context: "eating", Answer: "no"},
		{Context: "funeral", Answer: "unsure", Evidence: "white clothes, +20s"},
		// violence missing
	}
	f := SafetyFlags(negs, checks, nil, 100)
	if _, ok := f["eating"]; ok || f["funeral"] == "" || f["violence"] == "" {
		t.Fatalf("%v", f)
	}
	// Scene tags inside the window add a flag the model missed; outside are ignored.
	checks = []SafetyCheck{{Context: "eating", Answer: "no"}, {Context: "funeral", Answer: "no"}, {Context: "violence", Answer: "no"}}
	sc := []scenes.Scene{
		{Index: 3, Start: 60, End: 110, Contexts: []string{"Family eating dinner"}},
		{Index: 9, Start: 500, End: 600, Contexts: []string{"funeral"}},
	}
	f = SafetyFlags(negs, checks, sc, 100)
	if f["eating"] == "" || f["funeral"] != "" {
		t.Fatalf("%v", f)
	}
	var _ brands.Flags = f
}
