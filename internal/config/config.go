// Package config loads runtime configuration from the environment and prints it
// at startup so a setting that silently does nothing is visible immediately.
package config

import (
	"fmt"

	"github.com/thisizaro/adbreak/internal/ai"
	"github.com/thisizaro/adbreak/internal/pipeline"

	"io"
	"os"
	"strconv"
	"time"
)

type Pacing struct {
	MaxBreaksPerHour int
	MinGap           time.Duration
	MaxAdLoadPct     float64
	HeadMargin       time.Duration
	TailMargin       time.Duration
	SpeechMargin     time.Duration
	PodSeconds       float64
	MinScore         float64
}

type Config struct {
	Port        string
	Version     string
	DatabaseURL string
	GCSBucket   string
	GeminiKey   string
	GeminiURL   string
	GeminiModel string
	DecideModel string
	AIBackend   string
	GCPProject  string
	VertexLoc   string
	JobTokens   int64
	JobsPerDay  int
	GroqKey     string
	GroqURL     string
	ASRModel    string
	DataDir     string
	VideoDir    string
	BrandsPath  string
	SlateFont   string
	MaxUploadMB int
	UploadTo    string
	UploadPfx   string
	MaxUploadS  float64
	Pacing      Pacing
}

func Load() (Config, error) {
	var errs []error
	c := Config{
		Port:        str("PORT", "8080"),
		Version:     str("APP_VERSION", "dev"),
		DatabaseURL: str("DATABASE_URL", ""),
		GCSBucket:   str("GCS_BUCKET", ""),
		GeminiKey:   str("GEMINI_API_KEY", ""),
		GeminiURL:   str("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com/v1beta"),
		GeminiModel: str("GEMINI_MODEL", "gemini-3.1-flash-lite"),
		DecideModel: str("GEMINI_DECIDE_MODEL", "gemini-3.8-flash"),
		AIBackend:   str("AI_BACKEND", "vertex"),
		GCPProject:  str("GCP_PROJECT", ""),
		VertexLoc:   str("VERTEX_LOCATION", "global"),
		JobTokens:   int64(integer("JOB_TOKEN_CAP", 1500000, &errs)),
		JobsPerDay:  integer("JOBS_PER_DAY", 30, &errs),
		GroqKey:     str("GROQ_API_KEY", ""),
		GroqURL:     str("GROQ_BASE_URL", "https://api.groq.com/openai/v1"),
		ASRModel:    str("ASR_MODEL", "whisper-large-v3"),
		DataDir:     str("DATA_DIR", "data"),
		VideoDir:    str("VIDEO_DIR", "assets"),
		BrandsPath:  str("BRANDS_PATH", "assets/brands.json"),
		SlateFont:   str("SLATE_FONT", "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"),
		MaxUploadMB: integer("MAX_UPLOAD_MB", 700, &errs),
		UploadTo:    str("UPLOAD_TARGET", "local"),
		UploadPfx:   str("GCS_UPLOAD_PREFIX", "videos/"),
		MaxUploadS:  float("MAX_UPLOAD_SECONDS", 3600, &errs),
		Pacing: Pacing{
			MaxBreaksPerHour: integer("PACING_MAX_BREAKS_PER_HOUR", 6, &errs),
			MinGap:           dur("PACING_MIN_GAP", 6*time.Minute, &errs),
			MaxAdLoadPct:     float("PACING_MAX_AD_LOAD_PCT", 15, &errs),
			HeadMargin:       dur("PACING_HEAD_MARGIN", 3*time.Minute, &errs),
			TailMargin:       dur("PACING_TAIL_MARGIN", 2*time.Minute, &errs),
			SpeechMargin:     dur("SPEECH_MARGIN", 500*time.Millisecond, &errs),
			PodSeconds:       float("PACING_POD_SECONDS", 30, &errs),
			MinScore:         float("PACING_MIN_SCORE", 0.35, &errs),
		},
	}
	if c.UploadTo != "local" && !(c.UploadTo == "gcs" && c.GCSBucket != "") {
		errs = append(errs, fmt.Errorf("UPLOAD_TARGET=%q: want local, or gcs with GCS_BUCKET set", c.UploadTo))
	}
	if c.AIBackend != "vertex" && c.AIBackend != "studio" {
		errs = append(errs, fmt.Errorf("AI_BACKEND=%q: want vertex or studio", c.AIBackend))
	}
	if len(errs) > 0 {
		return c, fmt.Errorf("config: %v", errs)
	}
	return c, nil
}

// AISettings is the model backend configuration.
func (c Config) AISettings() ai.Settings {
	return ai.Settings{Backend: c.AIBackend, Project: c.GCPProject, Location: c.VertexLoc,
		APIKey: c.GeminiKey, BaseURL: c.GeminiURL, TokenBudget: c.JobTokens}
}

// PipelinePacing converts pacing to the plain seconds form the pipeline and UI use.
func (c Config) PipelinePacing() pipeline.Pacing {
	p := c.Pacing
	return pipeline.Pacing{
		MaxBreaksPerHour: float64(p.MaxBreaksPerHour), MinGap: p.MinGap.Seconds(), MaxAdLoadPct: p.MaxAdLoadPct,
		HeadMargin: p.HeadMargin.Seconds(), TailMargin: p.TailMargin.Seconds(), SpeechMargin: p.SpeechMargin.Seconds(),
		PodSeconds: p.PodSeconds, MinScore: p.MinScore,
	}
}

// Print writes the effective configuration. Secrets are reported as set or unset only.
func (c Config) Print(w io.Writer) {
	fmt.Fprintln(w, "effective config:")
	fmt.Fprintf(w, "  port=%s version=%s\n", c.Port, c.Version)
	fmt.Fprintf(w, "  upload_target=%s gcs_bucket=%q gcs_upload_prefix=%s\n", c.UploadTo, c.GCSBucket, c.UploadPfx)
	fmt.Fprintf(w, "  data_dir=%s video_dir=%s brands=%s slate_font=%s\n", c.DataDir, c.VideoDir, c.BrandsPath, c.SlateFont)
	fmt.Fprintf(w, "  budget guard: max_upload_mb=%d max_upload_seconds=%.0f jobs_per_day=%d job_token_cap=%d one job at a time\n", c.MaxUploadMB, c.MaxUploadS, c.JobsPerDay, c.JobTokens)
	fmt.Fprintf(w, "  ai_backend=%s gcp_project=%q vertex_location=%s gemini_api_key=%s\n", c.AIBackend, c.GCPProject, c.VertexLoc, secret(c.GeminiKey))
	fmt.Fprintf(w, "  models: bulk=%s decide=%s\n", c.GeminiModel, c.DecideModel)
	fmt.Fprintf(w, "  groq_api_key=%s asr_model=%s groq_url=%s\n", secret(c.GroqKey), c.ASRModel, c.GroqURL)
	p := c.Pacing
	fmt.Fprintf(w, "  pacing: max_breaks_per_hour=%d min_gap=%s max_ad_load_pct=%.1f head_margin=%s tail_margin=%s speech_margin=%s pod_seconds=%.0f min_score=%.2f\n",
		p.MaxBreaksPerHour, p.MinGap, p.MaxAdLoadPct, p.HeadMargin, p.TailMargin, p.SpeechMargin, p.PodSeconds, p.MinScore)
}

func secret(v string) string {
	if v == "" {
		return "UNSET"
	}
	return "set"
}

func str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func integer(key string, def int, errs *[]error) int {
	v := str(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s=%q: %w", key, v, err))
		return def
	}
	return n
}

func float(key string, def float64, errs *[]error) float64 {
	v := str(key, "")
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s=%q: %w", key, v, err))
		return def
	}
	return f
}

func dur(key string, def time.Duration, errs *[]error) time.Duration {
	v := str(key, "")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s=%q: %w", key, v, err))
		return def
	}
	return d
}
