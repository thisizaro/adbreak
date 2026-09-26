package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/oauth2/google"

	"github.com/thisizaro/adbreak/internal/app"
	"github.com/thisizaro/adbreak/internal/brands"
	"github.com/thisizaro/adbreak/internal/config"
	"github.com/thisizaro/adbreak/internal/jobs"
	"github.com/thisizaro/adbreak/internal/library"
	"github.com/thisizaro/adbreak/internal/pipeline"
	"github.com/thisizaro/adbreak/internal/server"
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

	factory, err := app.NewFactory(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	if p := factory.Project(); p != "" {
		log.Printf("vertex project: %s", p)
	}
	runner := jobs.NewRunner(jobs.NewBus(), func(ctx context.Context, j jobs.Job, progress func(stage, msg string)) (string, error) {
		video, err := lib.VideoPath(j.Episode)
		if err != nil {
			return "", err
		}
		ep := pipeline.Episode{ID: j.Episode, Video: video, Dir: filepath.Join(cfg.DataDir, j.Episode)}
		if err := os.MkdirAll(ep.Dir, 0o755); err != nil {
			return "", err
		}
		d := factory.Deps(progress)
		switch j.Kind {
		case "try-brand":
			var req struct {
				Brand brands.Brand `json:"brand"`
				Trial string       `json:"trial"`
			}
			if err := json.Unmarshal(j.Payload, &req); err != nil {
				return "", err
			}
			base, err := lib.Result(j.Episode)
			if err != nil {
				return "", err
			}
			progress("brands", fmt.Sprintf("re-placing %d scheduled breaks with %s added to %d brands", len(base.Breaks), req.Brand.ID, len(catalogue)))
			_, err = d.Rematch(ctx, ep, base, append(append([]brands.Brand(nil), catalogue...), req.Brand), req.Trial)
			return req.Trial, err
		default:
			_, err = d.Run(ctx, ep, catalogue, cfg.PipelinePacing())
			return "", err
		}
	})
	runner.PerDay = cfg.JobsPerDay
	if cfg.SelfURL != "" {
		pinger := &http.Client{Timeout: 10 * time.Second}
		runner.PingEvery = time.Minute
		runner.Ping = func() {
			if resp, err := pinger.Get(cfg.SelfURL + "/api/health"); err == nil {
				resp.Body.Close()
			}
		}
	}
	runner.Start(ctx, 8)

	var target server.UploadTarget = server.LocalTarget{}
	if cfg.UploadTo == "gcs" {
		ts, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/devstorage.read_write")
		if err != nil {
			log.Fatal(err)
		}
		target = server.GCSTarget{Bucket: cfg.GCSBucket, Prefix: cfg.UploadPfx, Tokens: ts}
	}
	handler := server.New(cfg.Version, static, lib, cfg.PipelinePacing()).WithUploads(&server.Uploads{
		Target: target, VideoDir: cfg.VideoDir, MaxBytes: int64(cfg.MaxUploadMB) << 20, MaxDuration: cfg.MaxUploadS, Runner: runner,
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
