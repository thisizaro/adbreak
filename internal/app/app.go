// Package app builds pipeline dependencies from config, shared by cmd/server and cmd/analyze.
package app

import (
	"context"

	"github.com/thisizaro/adbreak/internal/ai"
	"github.com/thisizaro/adbreak/internal/config"
	"github.com/thisizaro/adbreak/internal/pipeline"
	"github.com/thisizaro/adbreak/internal/speech"
)

// Factory hands out fresh pipeline deps per job so the token cap applies per job.
type Factory struct {
	cfg    config.Config
	vertex *ai.Vertex
}

func NewFactory(ctx context.Context, cfg config.Config) (*Factory, error) {
	f := &Factory{cfg: cfg}
	if cfg.AIBackend == "vertex" {
		v, err := ai.NewVertex(ctx, cfg.GCPProject, cfg.VertexLoc)
		if err != nil {
			return nil, err
		}
		f.vertex = v
	}
	return f, nil
}

// Project is the resolved GCP project (empty for the studio backend).
func (f *Factory) Project() string {
	if f.vertex == nil {
		return ""
	}
	return f.vertex.Project
}

func (f *Factory) Deps(progress func(stage, msg string)) pipeline.Deps {
	s := f.cfg.AISettings()
	return pipeline.Deps{
		ASR:            &speech.Groq{BaseURL: f.cfg.GroqURL, APIKey: f.cfg.GroqKey, Model: f.cfg.ASRModel, Language: "bn"},
		AI:             ai.New(s, f.vertex, f.cfg.GeminiModel),
		Decide:         ai.New(s, f.vertex, f.cfg.DecideModel),
		ShotThreshold:  0.3,
		ChunkSeconds:   600,
		OverlapSeconds: 5,
		ASRParallelism: 3,
		Progress:       progress,
	}
}
