// Package jobs runs pipeline jobs asynchronously. An in-process event bus
// carries job.created to a single worker, which is also the "one job at a
// time" budget guard. Job state is in memory today; the store seam (Postgres)
// is where it would persist once more than one instance exists.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Status string

const (
	Queued  Status = "queued"
	Running Status = "running"
	Done    Status = "done"
	Failed  Status = "failed"
)

type Job struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"` // "analyze" or "try-brand"
	Payload  json.RawMessage `json:"-"`
	Result   string          `json:"result,omitempty"`
	Episode  string          `json:"episode"`
	Status   Status          `json:"status"`
	Stage    string          `json:"stage"`
	Log      []string        `json:"log"`
	Error    string          `json:"error,omitempty"`
	Created  time.Time       `json:"created"`
	Finished time.Time       `json:"finished,omitzero"`
}

type Event struct {
	Topic string
	JobID string
}

// Bus is a minimal synchronous-publish, buffered-delivery event bus.
type Bus struct {
	mu   sync.RWMutex
	subs map[string][]chan Event
}

func NewBus() *Bus { return &Bus{subs: map[string][]chan Event{}} }

func (b *Bus) Subscribe(topic string, buf int) <-chan Event {
	ch := make(chan Event, buf)
	b.mu.Lock()
	b.subs[topic] = append(b.subs[topic], ch)
	b.mu.Unlock()
	return ch
}

// Publish returns an error instead of blocking when a subscriber is full.
func (b *Bus) Publish(e Event) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs[e.Topic] {
		select {
		case ch <- e:
		default:
			return fmt.Errorf("bus: %s subscriber full", e.Topic)
		}
	}
	return nil
}

// RunFunc executes one job, reporting progress, and returns a result reference.
type RunFunc func(ctx context.Context, j Job, progress func(stage, msg string)) (string, error)

type Runner struct {
	bus  *Bus
	run  RunFunc
	mu   sync.Mutex
	jobs map[string]*Job
	seq  int
	// PerDay caps jobs accepted in any rolling 24h window (0 = no cap). Counted
	// in memory, so a restart resets it; the Cloud Run instance is long-lived enough.
	PerDay int
	now    func() time.Time
	// Ping, if set, runs every PingEvery while a job executes. On Cloud Run it
	// requests the service's own URL so the instance is not scaled down mid-job
	// when the browser tab that started it has been closed.
	Ping      func()
	PingEvery time.Duration
}

// CapReached reports whether the rolling daily cap would reject a new job.
func (r *Runner) CapReached() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.capReachedLocked()
}

func (r *Runner) capReachedLocked() bool {
	if r.PerDay <= 0 {
		return false
	}
	n := 0
	for _, j := range r.jobs {
		if r.now().Sub(j.Created) < 24*time.Hour {
			n++
		}
	}
	return n >= r.PerDay
}

// ErrDailyCap is returned when the rolling 24h job cap is reached.
var ErrDailyCap = errors.New("daily job limit reached, try again tomorrow")

const TopicCreated = "job.created"

func NewRunner(bus *Bus, run RunFunc) *Runner {
	return &Runner{bus: bus, run: run, jobs: map[string]*Job{}, now: time.Now}
}

// Start consumes job.created events on one worker until ctx ends.
func (r *Runner) Start(ctx context.Context, queueSize int) {
	ch := r.bus.Subscribe(TopicCreated, queueSize)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-ch:
				r.execute(ctx, e.JobID)
			}
		}
	}()
}

func (r *Runner) Submit(episode string) (Job, error) { return r.SubmitKind("analyze", episode, nil) }

func (r *Runner) SubmitKind(kind, episode string, payload json.RawMessage) (Job, error) {
	r.mu.Lock()
	if r.capReachedLocked() {
		r.mu.Unlock()
		return Job{}, ErrDailyCap
	}
	r.seq++
	j := &Job{ID: fmt.Sprintf("job-%d-%d", time.Now().Unix(), r.seq), Kind: kind, Payload: payload, Episode: episode, Status: Queued, Created: r.now().UTC()}
	r.jobs[j.ID] = j
	snap := *j
	r.mu.Unlock()
	if err := r.bus.Publish(Event{Topic: TopicCreated, JobID: j.ID}); err != nil {
		r.update(j.ID, func(j *Job) { j.Status, j.Error = Failed, "queue full, try again later" })
		return snap, err
	}
	return snap, nil
}

func (r *Runner) Get(id string) (Job, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok {
		return Job{}, false
	}
	c := *j
	c.Log = append([]string(nil), j.Log...)
	return c, true
}

func (r *Runner) update(id string, f func(*Job)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j, ok := r.jobs[id]; ok {
		f(j)
	}
}

func (r *Runner) execute(ctx context.Context, id string) {
	j, ok := r.Get(id)
	if !ok {
		return
	}
	r.update(id, func(j *Job) { j.Status = Running })
	if r.Ping != nil && r.PingEvery > 0 {
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			t := time.NewTicker(r.PingEvery)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-t.C:
					r.Ping()
				}
			}
		}()
	}
	result, err := r.run(ctx, j, func(stage, msg string) {
		r.update(id, func(j *Job) { j.Stage = stage; j.Log = append(j.Log, stage+": "+msg) })
	})
	r.update(id, func(j *Job) {
		j.Finished = time.Now().UTC()
		if err != nil {
			j.Status, j.Error = Failed, err.Error()
		} else {
			j.Status, j.Stage, j.Result = Done, "done", result
		}
	})
}
