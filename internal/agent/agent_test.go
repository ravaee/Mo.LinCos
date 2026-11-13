package agent

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"
)

func TestIsLocalHost(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1:7070": true, "localhost:51234": true, "[::1]:7070": true, "LOCALHOST": true,
		"evil.example:7070": false, "10.0.0.5:7070": false, "": false,
	} {
		if got := IsLocalHost(host); got != want {
			t.Errorf("IsLocalHost(%q) = %v, want %v", host, got, want)
		}
	}
}

// A page on another site that points its own name at 127.0.0.1 must not read the API
func TestHandlerRefusesForeignHost(t *testing.T) {
	a := New(NewSampler(DefaultPaths(), "test"), time.Second, fstest.MapFS{"index.html": {Data: []byte("hi")}})
	h := a.Handler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "rebind.attacker.example:7070"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("foreign host: status %d, want 403", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:7070"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("local host: status %d, want 200", rec.Code)
	}
}

func TestStateKey(t *testing.T) {
	if stateKey("Up 7 days (healthy)") != stateKey("Up 8 days (healthy)") {
		t.Error("uptime ticking on must not count as a change")
	}
	if stateKey("Up 7 days (healthy)") == stateKey("Up 7 days (unhealthy)") {
		t.Error("a health change must count")
	}
	if stateKey("Up 2 hours") == stateKey("Exited (0) 1 minute ago") {
		t.Error("stopping must count")
	}
}

func TestBusiestKeepsCPUAndMemoryHogs(t *testing.T) {
	var all []Process
	for i := 0; i < 100; i++ {
		all = append(all, Process{PID: i, CPU: float64(i % 10), RSS: uint64(i)})
	}
	all = append(all, Process{PID: 999, CPU: 0, RSS: 1 << 30}) // idle but huge
	got := busiest(all, 10)
	if len(got) != 10 {
		t.Fatalf("kept %d, want 10", len(got))
	}
	found := false
	for _, p := range got {
		if p.PID == 999 {
			found = true
		}
	}
	if !found {
		t.Error("the idle process using the most memory was dropped")
	}
}

// A page on another site can make the browser POST to the agent's address, but
// not with the X-LinCos header, so nothing it sends can change anything
func TestChangesNeedTheHeader(t *testing.T) {
	a := New(NewSampler(DefaultPaths(), "test"), time.Second, fstest.MapFS{"index.html": {Data: []byte("hi")}})
	h := a.Handler()
	cases := []struct {
		header, origin string
		want           int
	}{
		{"", "", http.StatusForbidden},
		{"1", "http://evil.example", http.StatusForbidden},
		{"1", "http://127.0.0.1:7070", http.StatusNotFound}, // passes the guard; there is no container "x"
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/containers/x/files/delete?mount=0&path=a", nil)
		req.Host = "127.0.0.1:7070"
		if c.header != "" {
			req.Header.Set("X-LinCos", c.header)
		}
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("header %q origin %q: status %d, want %d", c.header, c.origin, rec.Code, c.want)
		}
	}
}
