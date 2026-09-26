package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/thisizaro/adbreak/internal/ai"
	"github.com/thisizaro/adbreak/internal/brands"
	"github.com/thisizaro/adbreak/internal/config"
	"github.com/thisizaro/adbreak/internal/jobs"
	"github.com/thisizaro/adbreak/internal/library"
	"github.com/thisizaro/adbreak/internal/pipeline"
	"github.com/thisizaro/adbreak/internal/server"
	"github.com/thisizaro/adbreak/internal/speech"
	"github.com/thisizaro/adbreak/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	cfg.Print(os.Stdout)

	static, err := web.Dist()
	if err != nil {
		log.Fatal(err)
	}

	catalogue, err := brands.Load(cfg.BrandsPath)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("catalogue: %d brands from %s", len(catalogue), cfg.BrandsPath)
	lib := &library.Library{DataDir: cfg.DataDir, VideoDir: cfg.VideoDir, Font: cfg.SlateFont,
		Catalogue: func() []brands.Brand { return catalogue }}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	deps := pipeline.Deps{
		ASR:            &speech.Groq{BaseURL: cfg.GroqURL, APIKey: cfg.GroqKey, Model: cfg.ASRModel, Language: "bn"},
		AI:             &ai.Gemini{BaseURL: cfg.GeminiURL, APIKey: cfg.GeminiKey, Model: cfg.GeminiModel},
		ShotThreshold:  0.3,
		ChunkSeconds:   600,
		OverlapSeconds: 5,
		ASRParallelism: 3,
	}
	runner := jobs.NewRunner(jobs.NewBus(), func(ctx context.Context, id string, progress func(stage, msg string)) error {
		video, err := lib.VideoPath(id)
		if err != nil {
			return err
		}
		ep := pipeline.Episode{ID: id, Video: video, Dir: filepath.Join(cfg.DataDir, id)}
		if err := os.MkdirAll(ep.Dir, 0o755); err != nil {
			return err
		}
		d := deps
		d.Progress = progress
		_, err = d.Run(ctx, ep, catalogue, cfg.PipelinePacing())
		return err
	})
	runner.Start(ctx, 8)

	handler := server.New(cfg.Version, static, lib, cfg.PipelinePacing()).WithUploads(&server.Uploads{
		VideoDir: cfg.VideoDir, MaxBytes: int64(cfg.MaxUploadMB) << 20, MaxDuration: cfg.MaxUploadS, Runner: runner,
	}).Handler()

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
