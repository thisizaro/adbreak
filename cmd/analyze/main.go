// Command analyze runs the pipeline on one local video. Dev tool; the server
// runs the same pipeline through the job runner.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/thisizaro/adbreak/internal/ai"
	"github.com/thisizaro/adbreak/internal/app"
	"github.com/thisizaro/adbreak/internal/brands"
	"github.com/thisizaro/adbreak/internal/config"
	"github.com/thisizaro/adbreak/internal/pipeline"
)

func main() {
	catalogueFlag := flag.String("brands", "assets/brands.json", "brand catalogue")
	flag.Parse()
	if flag.NArg() < 1 {
		log.Fatal("usage: analyze [-brands file] <video>")
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	cfg.Print(os.Stdout)
	video := flag.Arg(0)
	id := strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))
	ep := pipeline.Episode{ID: id, Video: video, Dir: filepath.Join("data", id)}
	if err := os.MkdirAll(ep.Dir, 0o755); err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	factory, err := app.NewFactory(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	d := factory.Deps(nil)
	catalogue, err := brands.Load(*catalogueFlag)
	if err != nil {
		log.Fatal(err)
	}
	res, err := d.Run(ctx, ep, catalogue, cfg.PipelinePacing())
	if err != nil {
		log.Fatal(err)
	}
	calls, tokens := d.AI.(*ai.Gemini).Usage()
	dc, dt := d.Decide.(*ai.Gemini).Usage()
	calls, tokens = calls+dc, tokens+dt
	log.Printf("funnel %+v", res.Funnel)
	for _, b := range res.Breaks {
		log.Printf("break t=%.2f score=%.2f brand=%s creative=%s", b.T, b.Score, b.Placement.Decision.BrandID, b.Placement.Decision.CreativeID)
	}
	log.Printf("gemini calls=%d tokens=%d (this run, cache hits excluded)", calls, tokens)
}
