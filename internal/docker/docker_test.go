package docker

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"testing"
)

const id = "0f3a9c2e7b1d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8091a2b3c4d5e6f708192a3b"

func TestReadUsage(t *testing.T) {
	u, ok := ReadUsage("testdata/cgroup", id)
	if !ok {
		t.Fatal("container cgroup not found")
	}
	if u.Memory != 220278784 || u.Limit != 1073741824 || u.Pids != 31 || u.CPUMicros != 34276569 {
		t.Errorf("usage = %+v", u)
	}
	if _, ok := ReadUsage("testdata/cgroup", "nope"); ok {
		t.Error("a missing container must report not found")
	}
}

// The client speaks HTTP over a Unix socket, as Docker does
func TestListAndInspectOverSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "docker.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Skip("unix sockets unavailable:", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /containers/json", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("all") != "1" {
			t.Error("stopped containers must be asked for too")
		}
		json.NewEncoder(w).Encode([]map[string]any{{
			"Id": id, "Names": []string{"/shop-web-1"}, "Image": "registry.local/shop:latest",
			"State": "running", "Status": "Up 7 days",
			"Labels": map[string]string{"com.docker.compose.project": "shop", "com.docker.compose.service": "webapp"},
		}})
	})
	mux.HandleFunc("GET /containers/{id}/json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Id":"` + id + `","RestartCount":2,"LogPath":"/var/lib/docker/containers/x/x-json.log",
			"State":{"Pid":4242,"StartedAt":"2026-11-06T10:00:00Z","Health":{"Status":"healthy"}},
			"HostConfig":{"LogConfig":{"Type":"json-file"}},
			"Mounts":[{"Type":"bind","Source":"/srv/shop/uploads","Destination":"/app/wwwroot/uploads","RW":true}]}`))
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(l)
	defer srv.Close()

	c := New(sock)
	list, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name() != "shop-web-1" || list[0].Project() != "shop" || list[0].Service() != "webapp" {
		t.Errorf("list = %+v", list)
	}
	d, err := c.Inspect(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if d.RestartCount != 2 || d.State.Health.Status != "healthy" || d.HostConfig.LogConfig.Type != "json-file" ||
		len(d.Mounts) != 1 || d.Mounts[0].Source != "/srv/shop/uploads" {
		t.Errorf("inspect = %+v", d)
	}
}

func TestMatters(t *testing.T) {
	for action, want := range map[string]bool{
		"start": true, "die": true, "destroy": true, "rename": true, "health_status: unhealthy": true, "restart": true,
		"exec_create: sqlcmd -Q SELECT 1": false, "exec_start: sqlcmd": false, "exec_die": false, "attach": false, "resize": false,
	} {
		if got := Matters(action); got != want {
			t.Errorf("Matters(%q) = %v, want %v", action, got, want)
		}
	}
}
