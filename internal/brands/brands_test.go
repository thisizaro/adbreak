package brands

import (
	"os"
	"path/filepath"
	"testing"
)

var cat = []Brand{
	{ID: "brand_a", Name: "Brand A", Target: []string{"eating"}, Negative: []string{"funeral", "bathroom"},
		Creatives: []Creative{{ID: "a15", Seconds: 15}, {ID: "a30", Seconds: 30}}},
	{ID: "brand_b", Name: "Brand B", Target: []string{"bathroom"}, Negative: []string{"eating"},
		Creatives: []Creative{{ID: "b20", Seconds: 20}}},
	// A ninth brand the code has never seen: nothing about it is special-cased.
	{ID: "brand_i", Name: "Brand I", Target: []string{"books"}, Negative: []string{"violence"},
		Creatives: []Creative{{ID: "i15", Seconds: 15}}},
}

func TestDecideBlocksOnYesOrUnsureInEitherScene(t *testing.T) {
	v := []Verdict{
		{BrandID: "brand_a", Fit: 0.9, DominantMatch: yes(), Negatives: []NegCheck{{Context: "funeral", Before: No, After: Unsure}, {Context: "bathroom", Before: No, After: No}}},
		{BrandID: "brand_b", Fit: 0.8, DominantMatch: yes(), Negatives: []NegCheck{{Context: "eating", Before: Yes, After: No}}},
		{BrandID: "brand_i", Fit: 0.4, DominantMatch: yes(), Negatives: []NegCheck{{Context: "violence", Before: No, After: No}}},
	}
	d := Decide(cat, v, nil, 30)
	if d.BrandID != "brand_i" || d.CreativeID != "i15" {
		t.Fatalf("picked %+v", d)
	}
	if len(d.Blocked) != 2 || d.Blocked["brand_a"] == "" || d.Blocked["brand_b"] == "" {
		t.Fatalf("blocked = %v", d.Blocked)
	}
}

func TestDecideTreatsMissingAnswersAsBlocked(t *testing.T) {
	v := []Verdict{
		// bathroom check omitted by the model
		{BrandID: "brand_a", Fit: 0.9, DominantMatch: yes(), Negatives: []NegCheck{{Context: "funeral", Before: No, After: No}}},
		// brand_b omitted entirely; brand_i clean but below the fit floor
		{BrandID: "brand_i", Fit: 0.05, DominantMatch: yes(), Negatives: []NegCheck{{Context: "violence", Before: No, After: No}}},
	}
	d := Decide(cat, v, nil, 30)
	if d.BrandID != "" || len(d.Blocked) != 2 {
		t.Fatalf("want no pick and 2 blocked, got %+v", d)
	}
}

func TestDecidePicksLongestCreativeThatFits(t *testing.T) {
	v := []Verdict{{BrandID: "brand_a", Fit: 0.9, DominantMatch: yes(), Negatives: []NegCheck{{Context: "funeral", Before: No, After: No}, {Context: "bathroom", Before: No, After: No}}}}
	if d := Decide(cat, v, nil, 20); d.CreativeID != "a15" {
		t.Fatalf("pod 20s should pick a15, got %+v", d)
	}
	if d := Decide(cat, v, nil, 30); d.CreativeID != "a30" {
		t.Fatalf("pod 30s should pick a30, got %+v", d)
	}
}

func TestLoadCatalogue(t *testing.T) {
	p := filepath.Join(t.TempDir(), "brands.json")
	os.WriteFile(p, []byte(`[{"brand_id":"brand_x","display_name":"Brand X","category":"c","target_contexts":["t"],"negative_contexts":["n"],"creatives":[{"id":"x15","duration_sec":15,"language":"bn","url":"ads/x.mp4"}]}]`), 0o644)
	got, err := Load(p)
	if err != nil || len(got) != 1 || got[0].Creatives[0].Seconds != 15 || got[0].Negative[0] != "n" {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestIndependentFlagBlocksEvenWhenPlacementModelSaysNo(t *testing.T) {
	// Placement model confidently says no funeral; the safety check disagrees.
	v := []Verdict{
		{BrandID: "brand_a", Fit: 0.9, DominantMatch: yes(), Negatives: []NegCheck{{Context: "funeral", Before: No, After: No}, {Context: "bathroom", Before: No, After: No}}},
		{BrandID: "brand_i", Fit: 0.5, DominantMatch: yes(), Negatives: []NegCheck{{Context: "violence", Before: No, After: No}}},
	}
	d := Decide(cat, v, Flags{"funeral": "safety model: yes, mourners at a cremation (+12s)"}, 30)
	if d.BrandID != "brand_i" || d.Blocked["brand_a"] == "" {
		t.Fatalf("%+v", d)
	}
}

func TestValidate(t *testing.T) {
	ok := Brand{ID: "brand_i", Name: "Brand I", Target: []string{"books"}, Negative: []string{"violence"},
		Creatives: []Creative{{ID: "i_20s_bn", Seconds: 20}}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := []Brand{
		{ID: "../x", Name: "x", Target: []string{"a"}, Creatives: ok.Creatives},
		{ID: "b", Name: "", Target: []string{"a"}, Creatives: ok.Creatives},
		{ID: "bb", Name: "B", Target: nil, Creatives: ok.Creatives},
		{ID: "bb", Name: "B", Target: []string{"a"}, Creatives: []Creative{{ID: "c", Seconds: 999}}},
	}
	for i, b := range bad {
		if b.Validate() == nil {
			t.Errorf("case %d should fail", i)
		}
	}
}

func yes() *bool { b := true; return &b }
func no() *bool  { b := false; return &b }

func TestDominantActivityRequiredAndMinFit(t *testing.T) {
	clean := []NegCheck{{Context: "funeral", Before: No, After: No}, {Context: "bathroom", Before: No, After: No}}
	cases := []struct {
		name string
		v    Verdict
		want string
	}{
		{"incidental match never places", Verdict{BrandID: "brand_a", Fit: 0.9, DominantMatch: no(), Negatives: clean}, ""},
		{"missing dominant answer counts as no", Verdict{BrandID: "brand_a", Fit: 0.9, Negatives: clean}, ""},
		{"weak fit below floor", Verdict{BrandID: "brand_a", Fit: 0.3, DominantMatch: yes(), Negatives: clean}, ""},
		{"dominant match above floor places", Verdict{BrandID: "brand_a", Fit: 0.5, DominantMatch: yes(), Negatives: clean}, "brand_a"},
	}
	for _, c := range cases {
		d := Decide(cat[:1], []Verdict{c.v}, nil, 30)
		if d.BrandID != c.want {
			t.Errorf("%s: got %q (unfit %v)", c.name, d.BrandID, d.Unfit)
		}
		if c.want == "" && d.Unfit["brand_a"] == "" {
			t.Errorf("%s: no unfit reason recorded", c.name)
		}
	}
}
