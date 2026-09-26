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
	"github.com/thisizaro/adbreak/internal/brands"
	"github.com/thisizaro/adbreak/internal/config"
	"github.com/thisizaro/adbreak/internal/pipeline"
	"github.com/thisizaro/adbreak/internal/speech"
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
	d := pipeline.Deps{
		ASR:            &speech.Groq{BaseURL: cfg.GroqURL, APIKey: cfg.GroqKey, Model: cfg.ASRModel, Language: "bn"},
		ShotThreshold:  0.3,
		ChunkSeconds:   600,
		OverlapSeconds: 5,
		ASRParallelism: 3,
	}
	catalogue, err := brands.Load(*catalogueFlag)
	if err != nil {
		log.Fatal(err)
	}
	gem := &ai.Gemini{BaseURL: cfg.GeminiURL, APIKey: cfg.GeminiKey, Model: cfg.GeminiModel}
	d.AI = gem
	res, err := d.Run(context.Background(), ep, catalogue, cfg.PipelinePacing())
	if err != nil {
		log.Fatal(err)
	}
	calls, tokens := gem.Usage()
	log.Printf("funnel %+v", res.Funnel)
	for _, b := range res.Breaks {
		log.Printf("break t=%.2f score=%.2f brand=%s creative=%s", b.T, b.Score, b.Placement.Decision.BrandID, b.Placement.Decision.CreativeID)
	}
	log.Printf("gemini calls=%d tokens=%d (this run, cache hits excluded)", calls, tokens)
}
