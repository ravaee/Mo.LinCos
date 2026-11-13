package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Agent samples the machine while someone is watching and streams the snapshots
// to every open page. With no page open it does nothing at all.
type Agent struct {
	sampler  *Sampler
	interval time.Duration
	ui       fs.FS

	mu     sync.Mutex
	subs   map[chan []byte]struct{}
	latest []byte
	wake   chan struct{}
	sample sync.Mutex // one snapshot at a time
}

func New(sampler *Sampler, interval time.Duration, ui fs.FS) *Agent {
	return &Agent{sampler: sampler, interval: interval, ui: ui, subs: map[chan []byte]struct{}{}, wake: make(chan struct{}, 1)}
}

// Run samples every interval while at least one page is open.
func (a *Agent) Run(ctx context.Context) {
	go a.sampler.WatchDocker(ctx)
	t := time.NewTicker(a.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-a.wake:
		}
		if a.watchers() == 0 {
			continue
		}
		a.publish(a.fresh(ctx))
	}
}

// fresh takes a snapshot. After a pause its rates would be averages over the
// whole pause, so it takes a second one shortly after and returns that.
func (a *Agent) fresh(ctx context.Context) Snapshot {
	a.sample.Lock()
	defer a.sample.Unlock()
	stale := time.Since(a.sampler.last) > 3*a.interval
	snap := a.sampler.Take(ctx)
	if stale {
		select {
		case <-ctx.Done():
		case <-time.After(500 * time.Millisecond):
		}
		snap = a.sampler.Take(ctx)
	}
	snap.Agent.Interval = a.interval.Seconds()
	return snap
}

func (a *Agent) watchers() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.subs)
}

func (a *Agent) publish(snap Snapshot) {
	data, err := json.Marshal(snap)
	if err != nil {
		log.Printf("snapshot: %v", err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.latest = data
	for ch := range a.subs {
		select {
		case ch <- data:
		default: // a slow page skips a frame rather than holding up the rest
		}
	}
}

func (a *Agent) subscribe() (chan []byte, []byte) {
	ch := make(chan []byte, 1)
	a.mu.Lock()
	a.subs[ch] = struct{}{}
	latest := a.latest
	a.mu.Unlock()
	select {
	case a.wake <- struct{}{}:
	default:
	}
	return ch, latest
}

func (a *Agent) unsubscribe(ch chan []byte) {
	a.mu.Lock()
	delete(a.subs, ch)
	if len(a.subs) == 0 {
		a.latest = nil // stale by the time anyone looks again
	}
	a.mu.Unlock()
}

// Handler serves the page and the API.
func (a *Agent) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(a.ui))
	mux.HandleFunc("GET /api/snapshot", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(a.fresh(r.Context()))
	})
	mux.HandleFunc("GET /api/stream", a.stream)
	return localOnly(mux)
}

// stream sends a snapshot to the page every interval as Server-Sent Events.
func (a *Agent) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	ch, latest := a.subscribe()
	defer a.unsubscribe(ch)
	send := func(data []byte) bool {
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if latest != nil && !send(latest) {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case data := <-ch:
			if !send(data) {
				return
			}
		}
	}
}

// localOnly refuses requests whose Host is not this machine. The agent listens on
// 127.0.0.1 only, but a web page in your browser could still point a name of its
// own at 127.0.0.1 (DNS rebinding) and read the API through your SSH tunnel; the
// Host header gives that away.
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !IsLocalHost(r.Host) {
			http.Error(w, "forbidden: open LinCos through 127.0.0.1 or localhost", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// IsLocalHost says whether a Host header names this machine.
func IsLocalHost(hostport string) bool {
	h, _, err := net.SplitHostPort(hostport)
	if err != nil {
		h = hostport
	}
	h = strings.Trim(h, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
