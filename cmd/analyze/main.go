// Command analyze runs the pipeline on one local video. Dev tool; the server
// runs the same pipeline through the job runner.
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/thisizaro/adbreak/internal/config"
	"github.com/thisizaro/adbreak/internal/pipeline"
	"github.com/thisizaro/adbreak/internal/speech"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: analyze <video>")
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	cfg.Print(os.Stdout)
	video := os.Args[1]
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
	ctx := context.Background()
	if _, err := d.Media(ctx, ep); err != nil {
		log.Fatal(err)
	}
	if _, err := d.Transcript(ctx, ep); err != nil {
		log.Fatal(err)
	}
}
