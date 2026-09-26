// Package config loads runtime configuration from the environment and prints it
// at startup so a setting that silently does nothing is visible immediately.
package config

import (
	"fmt"
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
}

type Config struct {
	Port        string
	Version     string
	DatabaseURL string
	GCSBucket   string
	GeminiKey   string
	GroqKey     string
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
		GroqKey:     str("GROQ_API_KEY", ""),
		Pacing: Pacing{
			MaxBreaksPerHour: integer("PACING_MAX_BREAKS_PER_HOUR", 4, &errs),
			MinGap:           dur("PACING_MIN_GAP", 6*time.Minute, &errs),
			MaxAdLoadPct:     float("PACING_MAX_AD_LOAD_PCT", 15, &errs),
			HeadMargin:       dur("PACING_HEAD_MARGIN", 3*time.Minute, &errs),
			TailMargin:       dur("PACING_TAIL_MARGIN", 2*time.Minute, &errs),
			SpeechMargin:     dur("SPEECH_MARGIN", 500*time.Millisecond, &errs),
		},
	}
	if len(errs) > 0 {
		return c, fmt.Errorf("config: %v", errs)
	}
	return c, nil
}

// Print writes the effective configuration. Secrets are reported as set or unset only.
func (c Config) Print(w io.Writer) {
	fmt.Fprintln(w, "effective config:")
	fmt.Fprintf(w, "  port=%s version=%s\n", c.Port, c.Version)
	fmt.Fprintf(w, "  database_url=%s gcs_bucket=%q\n", secret(c.DatabaseURL), c.GCSBucket)
	fmt.Fprintf(w, "  gemini_api_key=%s groq_api_key=%s\n", secret(c.GeminiKey), secret(c.GroqKey))
	p := c.Pacing
	fmt.Fprintf(w, "  pacing: max_breaks_per_hour=%d min_gap=%s max_ad_load_pct=%.1f head_margin=%s tail_margin=%s speech_margin=%s\n",
		p.MaxBreaksPerHour, p.MinGap, p.MaxAdLoadPct, p.HeadMargin, p.TailMargin, p.SpeechMargin)
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
