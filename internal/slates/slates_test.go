package slates

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/thisizaro/adbreak/internal/media"
)

func TestRenderExactDuration(t *testing.T) {
	font := "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"
	if _, err := os.Stat(font); err != nil {
		t.Skip("font not installed")
	}
	out := filepath.Join(t.TempDir(), "s.mp4")
	spec := Spec{BrandName: "Brand: Test's", Category: "food/spices", Seconds: 3, Width: 320, Height: 180, Font: font}
	if err := Render(context.Background(), spec, out); err != nil {
		t.Fatal(err)
	}
	info, err := media.Probe(context.Background(), out)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(info.Duration-3) > 0.1 || !info.HasAudio || info.Width != 320 {
		t.Fatalf("got %+v", info)
	}
}
