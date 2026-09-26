package manifest

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestClock(t *testing.T) {
	if got := Clock(917.0, true); got != "00:15:17.000" {
		t.Fatal(got)
	}
	if got := Clock(3725.4567, true); got != "01:02:05.457" {
		t.Fatal(got)
	}
	if got := Clock(30, false); got != "00:00:30" {
		t.Fatal(got)
	}
}

func TestVMAPRoundTrip(t *testing.T) {
	out, err := VMAP([]Ad{
		{BreakID: "break-1", Offset: 917, AdID: "a_30s_bn", Title: "Brand A", Seconds: 30, MediaURL: "https://x/a.mp4?sig=1&b=2", Width: 960, Height: 540, Impression: "https://x/imp"},
		{BreakID: "break-2", Offset: 1500.25, AdID: "b_20s_bn", Title: "Brand B", Seconds: 20, MediaURL: "https://x/b.mp4", Width: 960, Height: 540, Impression: "https://x/imp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{`xmlns:vmap="http://www.iab.net/videosuite/vmap"`, `timeOffset="00:15:17.000"`, `timeOffset="00:25:00.250"`,
		`breakType="linear"`, `<VAST version="3.0">`, `<Duration>00:00:30</Duration>`, `<![CDATA[https://x/a.mp4?sig=1&b=2]]>`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in\n%s", want, s)
		}
	}
	// Well-formed: a generic decoder must walk the whole document.
	d := xml.NewDecoder(strings.NewReader(s))
	n := 0
	for {
		tok, err := d.Token()
		if tok == nil || err != nil {
			if err != nil && err.Error() != "EOF" {
				t.Fatalf("not well-formed: %v", err)
			}
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "MediaFile" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("want 2 MediaFile elements, got %d", n)
	}
}
