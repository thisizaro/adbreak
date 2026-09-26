package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/thisizaro/adbreak/internal/brands"
	"github.com/thisizaro/adbreak/internal/jobs"
	"github.com/thisizaro/adbreak/internal/library"
	"github.com/thisizaro/adbreak/internal/pipeline"
)

func TestLocalUploadFinalizeAndJob(t *testing.T) {
	data, videos := t.TempDir(), t.TempDir()
	clip := filepath.Join(t.TempDir(), "c.mp4")
	if b, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=red:s=160x90:d=2",
		"-f", "lavfi", "-i", "sine=d=2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", clip).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, b)
	}
	ran := make(chan string, 1)
	runner := jobs.NewRunner(jobs.NewBus(), func(_ context.Context, ep string, _ func(string, string)) error { ran <- ep; return nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx, 2)
	lib := &library.Library{DataDir: data, VideoDir: videos, Catalogue: func() []brands.Brand { return nil }}
	h := New("t", fstest.MapFS{"index.html": {Data: []byte("x")}}, lib, pipeline.Pacing{}).WithUploads(&Uploads{
		Target: LocalTarget{}, VideoDir: videos, MaxBytes: 10 << 20, MaxDuration: 60, Runner: runner}).Handler()

	do := func(method, path string, body []byte) (int, map[string]any) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, path, bytes.NewReader(body)))
		var m map[string]any
		json.Unmarshal(rec.Body.Bytes(), &m)
		return rec.Code, m
	}
	code, up := do("POST", "/api/uploads", nil)
	id, _ := up["id"].(string)
	if code != 201 || !strings.HasPrefix(up["upload_url"].(string), "/api/uploads/up_") {
		t.Fatalf("create: %d %v", code, up)
	}
	if code, _ := do("POST", "/api/uploads/"+id+"/finalize", nil); code != 404 {
		t.Fatalf("finalize before upload should 404, got %d", code)
	}
	b, _ := os.ReadFile(clip)
	if code, m := do("PUT", up["upload_url"].(string), b); code != 200 {
		t.Fatalf("put: %d %v", code, m)
	}
	if code, m := do("POST", "/api/uploads/"+id+"/finalize", nil); code != 200 || m["duration"].(float64) < 1.5 {
		t.Fatalf("finalize: %d %v", code, m)
	}
	if code, m := do("POST", "/api/jobs", []byte(`{"episode":"`+id+`"}`)); code != 202 {
		t.Fatalf("job: %d %v", code, m)
	}
	if got := <-ran; got != id {
		t.Fatalf("ran %q", got)
	}
	// A non-video is rejected at finalize and removed.
	_, up2 := do("POST", "/api/uploads", nil)
	id2 := up2["id"].(string)
	do("PUT", up2["upload_url"].(string), []byte("not a video"))
	if code, _ := do("POST", "/api/uploads/"+id2+"/finalize", nil); code != 422 {
		t.Fatalf("junk finalize: %d", code)
	}
	if _, err := os.Stat(filepath.Join(videos, id2+".mp4")); !os.IsNotExist(err) {
		t.Fatal("rejected upload not removed")
	}
	_ = http.StatusOK
}
