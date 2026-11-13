// Package docker lists containers through the Docker socket and reads their CPU
// and memory straight from cgroups, which is far cheaper than the stats API.
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ravaee/Mo.LinCos/internal/host"
)

// Client talks to the Docker Engine API over its Unix socket.
type Client struct {
	http *http.Client
}

func New(socket string) *Client {
	return &Client{http: &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socket)
			},
			MaxIdleConns: 2,
		},
	}}
}

func (c *Client) get(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("docker %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// Summary is one row of GET /containers/json.
type Summary struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	State   string            `json:"State"`  // running, exited, restarting, ...
	Status  string            `json:"Status"` // "Up 7 days (healthy)"
	Created int64             `json:"Created"`
	Labels  map[string]string `json:"Labels"`
}

// List returns every container, stopped ones included.
func (c *Client) List(ctx context.Context) ([]Summary, error) {
	var out []Summary
	err := c.get(ctx, "/containers/json?all=1", &out)
	return out, err
}

// Details is the part of GET /containers/{id}/json the agent uses.
type Details struct {
	ID           string `json:"Id"`
	RestartCount int    `json:"RestartCount"`
	LogPath      string `json:"LogPath"`
	State        struct {
		Pid       int    `json:"Pid"`
		StartedAt string `json:"StartedAt"`
		ExitCode  int    `json:"ExitCode"`
		Health    *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
	HostConfig struct {
		LogConfig struct {
			Type string `json:"Type"`
		} `json:"LogConfig"`
	} `json:"HostConfig"`
	Mounts []Mount `json:"Mounts"`
}

// Mount is a folder or volume bound into a container. Source is the path on the
// host, so its log files can be found there.
type Mount struct {
	Type        string `json:"Type"` // bind or volume
	Name        string `json:"Name,omitempty"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	RW          bool   `json:"RW"`
}

func (c *Client) Inspect(ctx context.Context, id string) (Details, error) {
	var d Details
	err := c.get(ctx, "/containers/"+url.PathEscape(id)+"/json", &d)
	return d, err
}

// Name is the container's name without Docker's leading slash.
func (s Summary) Name() string {
	if len(s.Names) == 0 {
		return s.ID[:12]
	}
	return strings.TrimPrefix(s.Names[0], "/")
}

// Project and Service are the compose stack and service that made the container,
// empty when it was started by hand.
func (s Summary) Project() string { return s.Labels["com.docker.compose.project"] }
func (s Summary) Service() string { return s.Labels["com.docker.compose.service"] }

// Usage is a running container's resource use from its cgroup.
type Usage struct {
	CPUMicros uint64 // CPU time so far; usage is the change between readings
	Memory    uint64 // bytes in use, without cache the kernel can drop
	Limit     uint64 // the memory limit, 0 when there is none
	Pids      int
}

// ReadUsage reads a container's cgroup under the systemd driver
// (system.slice/docker-<id>.scope) or the cgroupfs driver (docker/<id>).
func ReadUsage(cgroupRoot, id string) (Usage, bool) {
	var u Usage
	dir := ""
	for _, d := range []string{
		filepath.Join(cgroupRoot, "system.slice", "docker-"+id+".scope"),
		filepath.Join(cgroupRoot, "docker", id),
	} {
		if _, err := os.Stat(d); err == nil {
			dir = d
			break
		}
	}
	if dir == "" {
		return u, false
	}
	u.Memory, _ = host.CgroupMemory(dir)
	u.Limit = readUint(filepath.Join(dir, "memory.max")) // "max" reads as 0
	u.Pids = int(readUint(filepath.Join(dir, "pids.current")))
	u.CPUMicros, _ = host.CgroupCPU(dir)
	return u, true
}

func readUint(path string) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	v, _ := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	return v
}

// Watch calls changed whenever a container is created, started, stopped,
// restarted, renamed, removed or changes health, by following Docker's event
// stream. It reconnects until ctx ends, and calls changed after each reconnect,
// since events may have been missed.
//
// Listening costs Docker nothing while nothing happens; asking for the container
// list every few seconds instead costs it about 40 ms of CPU per call.
func (c *Client) Watch(ctx context.Context, changed func()) {
	stream := &http.Client{Transport: c.http.Transport} // no timeout: the stream stays open
	filters := url.QueryEscape(`{"type":["container"]}`)
	for ctx.Err() == nil {
		c.follow(ctx, stream, "http://docker/events?filters="+filters, changed)
		changed()
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

func (c *Client) follow(ctx context.Context, stream *http.Client, url string, changed func()) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	resp, err := stream.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	for {
		var ev struct {
			Action string `json:"Action"`
		}
		if err := dec.Decode(&ev); err != nil {
			return
		}
		if Matters(ev.Action) {
			changed()
		}
	}
}

// Matters says whether a container event changes what the list shows. Health
// checks run as exec commands every few seconds; those events are ignored.
func Matters(action string) bool {
	if strings.HasPrefix(action, "exec_") {
		return false
	}
	switch strings.SplitN(action, ":", 2)[0] {
	case "attach", "detach", "resize", "top", "copy", "export", "archive-path", "extract-to-dir", "commit":
		return false
	}
	return true
}
