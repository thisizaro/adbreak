package jobs

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func wait(t *testing.T, r *Runner, id string, want Status) Job {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if j, _ := r.Get(id); j.Status == want {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	j, _ := r.Get(id)
	t.Fatalf("job %s status %s, want %s", id, j.Status, want)
	return j
}

func TestRunnerRunsOneAtATimeAndRecordsProgress(t *testing.T) {
	var running, maxRunning atomic.Int32
	run := func(ctx context.Context, ep string, progress func(string, string)) error {
		n := running.Add(1)
		if n > maxRunning.Load() {
			maxRunning.Store(n)
		}
		defer running.Add(-1)
		progress("media", "probing "+ep)
		time.Sleep(20 * time.Millisecond)
		if ep == "bad" {
			return errors.New("boom")
		}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := NewRunner(NewBus(), run)
	r.Start(ctx, 4)
	a, _ := r.Submit("ep1")
	b, _ := r.Submit("bad")
	ja := wait(t, r, a.ID, Done)
	jb := wait(t, r, b.ID, Failed)
	if len(ja.Log) != 1 || ja.Log[0] != "media: probing ep1" || jb.Error != "boom" {
		t.Fatalf("a=%+v b=%+v", ja, jb)
	}
	if maxRunning.Load() != 1 {
		t.Fatalf("ran %d jobs concurrently", maxRunning.Load())
	}
}

func TestSubmitFailsWhenQueueFull(t *testing.T) {
	block := make(chan struct{})
	r := NewRunner(NewBus(), func(context.Context, string, func(string, string)) error { <-block; return nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer close(block)
	r.Start(ctx, 1)
	r.Submit("a") // picked up by the worker
	time.Sleep(20 * time.Millisecond)
	r.Submit("b") // fills the buffer
	if _, err := r.Submit("c"); err == nil {
		t.Fatal("expected queue full error")
	}
}

func TestDailyCap(t *testing.T) {
	r := NewRunner(NewBus(), func(context.Context, string, func(string, string)) error { return nil })
	r.PerDay = 2
	now := time.Now()
	r.now = func() time.Time { return now }
	r.Submit("a")
	r.Submit("b")
	if _, err := r.Submit("c"); !errors.Is(err, ErrDailyCap) {
		t.Fatalf("want ErrDailyCap, got %v", err)
	}
	now = now.Add(25 * time.Hour)
	if _, err := r.Submit("d"); err != nil {
		t.Fatalf("cap should roll over after 24h: %v", err)
	}
}
