package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/thisizaro/adbreak/internal/brands"
	"github.com/thisizaro/adbreak/internal/config"
	"github.com/thisizaro/adbreak/internal/library"
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

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           server.New(cfg.Version, static, lib, cfg.PipelinePacing()).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
