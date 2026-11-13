package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ravaee/Mo.LinCos/internal/database"
	"github.com/ravaee/Mo.LinCos/internal/docker"
	"github.com/ravaee/Mo.LinCos/internal/files"
	"github.com/ravaee/Mo.LinCos/internal/systemd"
)

// routes adds the pages of one container or service: details, logs, files and database.
func (a *Agent) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/containers/{id}", a.containerView)
	mux.HandleFunc("GET /api/containers/{id}/logs", a.containerLogs)
	mux.HandleFunc("GET /api/containers/{id}/logs/stream", a.containerLogStream)
	mux.HandleFunc("GET /api/containers/{id}/files", a.fileList)
	mux.HandleFunc("GET /api/containers/{id}/files/raw", a.fileRaw)
	mux.HandleFunc("GET /api/containers/{id}/files/usage", a.fileUsage)
	mux.HandleFunc("POST /api/containers/{id}/files/delete", a.fileDelete)
	mux.HandleFunc("POST /api/containers/{id}/files/upload", a.fileUpload)
	mux.HandleFunc("POST /api/containers/{id}/db/{action}", a.db)
	mux.HandleFunc("GET /api/services/{name}/logs", a.serviceLogs)
	mux.HandleFunc("GET /api/services/{name}/logs/stream", a.serviceLogStream)
}

// changesGuard lets a request that changes something through only when it
// carries the X-LinCos header. A page on another site can make your browser
// send a form to 127.0.0.1, but it can't add a custom header without the
// agent's permission, which it never gives.
func changesGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("X-LinCos") != "1" {
				http.Error(w, "forbidden: changes come only from the LinCos page", http.StatusForbidden)
				return
			}
			if o := r.Header.Get("Origin"); o != "" {
				if u, err := url.Parse(o); err != nil || u.Host != r.Host {
					http.Error(w, "forbidden: wrong origin", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

// audit writes every change to the agent's log, so journalctl -u lincos shows
// who changed what.
func audit(r *http.Request, format string, args ...any) {
	log.Printf("change: "+format, args...)
}

func (a *Agent) inspect(r *http.Request) (docker.Details, error) {
	return a.sampler.docker.Inspect(r.Context(), r.PathValue("id"))
}

// ---------- container ----------

// ContainerView is a container's page: what it is and how it runs. The values
// of its environment are left out, since they hold passwords.
type ContainerView struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	Image      string           `json:"image"`
	Created    string           `json:"created"`
	Status     string           `json:"status"`
	StartedAt  string           `json:"startedAt"`
	FinishedAt string           `json:"finishedAt"`
	ExitCode   int              `json:"exitCode"`
	OOMKilled  bool             `json:"oomKilled"`
	Error      string           `json:"error,omitempty"`
	Health     string           `json:"health,omitempty"`
	HealthLog  string           `json:"healthLog,omitempty"` // the last health check's output
	Restarts   int              `json:"restarts"`
	Restart    string           `json:"restartPolicy"`
	Command    string           `json:"command"`
	WorkingDir string           `json:"workingDir,omitempty"`
	User       string           `json:"user,omitempty"`
	EnvNames   []string         `json:"envNames"`
	Project    string           `json:"project,omitempty"`
	Service    string           `json:"service,omitempty"`
	ComposeDir string           `json:"composeDir,omitempty"`
	Networks   []string         `json:"networks"`
	IP         string           `json:"ip,omitempty"`
	Ports      []string         `json:"ports"`
	Mounts     []docker.Mount   `json:"mounts"`
	LogDriver  string           `json:"logDriver"`
	LogPath    string           `json:"logPath,omitempty"`
	MemLimit   int64            `json:"memLimit"`
	Database   *database.Target `json:"database,omitempty"`
}

func (a *Agent) containerView(w http.ResponseWriter, r *http.Request) {
	d, err := a.inspect(r)
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	v := ContainerView{
		ID: d.ID, Name: strings.TrimPrefix(d.Name, "/"), Image: d.Config.Image, Created: d.Created,
		Status: d.State.Status, StartedAt: d.State.StartedAt, FinishedAt: d.State.FinishedAt, ExitCode: d.State.ExitCode,
		OOMKilled: d.State.OOMKilled, Error: d.State.Error, Restarts: d.RestartCount, Restart: d.HostConfig.RestartPolicy.Name,
		Command: strings.Join(append(d.Config.Entrypoint, d.Config.Cmd...), " "), WorkingDir: d.Config.WorkingDir, User: d.Config.User,
		Project: d.Config.Labels["com.docker.compose.project"], Service: d.Config.Labels["com.docker.compose.service"],
		ComposeDir: d.Config.Labels["com.docker.compose.project.working_dir"],
		IP:         d.IP(), Mounts: d.Mounts, LogDriver: d.HostConfig.LogConfig.Type, LogPath: d.LogPath, MemLimit: d.HostConfig.Memory,
		EnvNames: []string{}, Networks: []string{}, Ports: []string{},
	}
	if v.Mounts == nil {
		v.Mounts = []docker.Mount{}
	}
	docker.SortMounts(v.Mounts)
	if h := d.State.Health; h != nil {
		v.Health = h.Status
		if n := len(h.Log); n > 0 {
			v.HealthLog = strings.TrimSpace(h.Log[n-1].Output)
		}
	}
	for _, kv := range d.Config.Env {
		k, _, _ := strings.Cut(kv, "=")
		v.EnvNames = append(v.EnvNames, k)
	}
	sort.Strings(v.EnvNames)
	for n := range d.NetworkSettings.Networks {
		v.Networks = append(v.Networks, n)
	}
	sort.Strings(v.Networks)
	for port, binds := range d.NetworkSettings.Ports {
		if len(binds) == 0 {
			v.Ports = append(v.Ports, port+" (inside only)")
		}
		for _, b := range binds {
			v.Ports = append(v.Ports, fmt.Sprintf("%s:%s → %s", b.HostIP, b.HostPort, port))
		}
	}
	sort.Strings(v.Ports)
	if t, ok := database.Detect(d.Config.Image, d.IP(), d.Env); ok {
		v.Database = &t
	}
	writeJSON(w, http.StatusOK, v)
}

// ---------- logs ----------

const maxLogLines = 5000

func logCount(r *http.Request, def int) int {
	n, err := strconv.Atoi(r.URL.Query().Get("tail"))
	if err != nil || n <= 0 {
		return def
	}
	return min(n, maxLogLines)
}

// containerLogs returns lines from further back: the last `tail` before `until`.
func (a *Agent) containerLogs(w http.ResponseWriter, r *http.Request) {
	d, err := a.inspect(r)
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	lines := []docker.Line{}
	err = a.sampler.docker.Logs(r.Context(), d.ID, d.Config.Tty, docker.LogOptions{Tail: logCount(r, 500), Until: r.URL.Query().Get("until")},
		func(l docker.Line) bool { lines = append(lines, l); return true })
	if err != nil {
		fail(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, lines)
}

// containerLogStream sends the last lines and then new ones as they come.
func (a *Agent) containerLogStream(w http.ResponseWriter, r *http.Request) {
	d, err := a.inspect(r)
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	streamLines(w, r, func(ctx context.Context, emit func(any) bool) error {
		return a.sampler.docker.Logs(ctx, d.ID, d.Config.Tty, docker.LogOptions{Tail: logCount(r, 500), Follow: true},
			func(l docker.Line) bool { return emit(l) })
	})
}

var unitName = regexp.MustCompile(`^[A-Za-z0-9@._:-]+\.service$`)

func (a *Agent) serviceLogs(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !unitName.MatchString(name) {
		fail(w, http.StatusBadRequest, errors.New("not a service name"))
		return
	}
	lines := []systemd.Entry{}
	err := systemd.Journal(r.Context(), systemd.JournalOptions{Unit: name, Lines: logCount(r, 500), Before: r.URL.Query().Get("before")},
		func(e systemd.Entry) bool { lines = append(lines, e); return true })
	if err != nil {
		fail(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, lines)
}

func (a *Agent) serviceLogStream(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !unitName.MatchString(name) {
		fail(w, http.StatusBadRequest, errors.New("not a service name"))
		return
	}
	streamLines(w, r, func(ctx context.Context, emit func(any) bool) error {
		return systemd.Journal(ctx, systemd.JournalOptions{Unit: name, Lines: logCount(r, 500), Follow: true},
			func(e systemd.Entry) bool { return emit(e) })
	})
}

// streamLines sends log lines as Server-Sent Events, gathered into one event
// every quarter second, so a busy log is a few events a second and not thousands.
func streamLines(w http.ResponseWriter, r *http.Request, read func(ctx context.Context, emit func(any) bool) error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	lines := make(chan any, 1024)
	done := make(chan error, 1)
	go func() {
		done <- read(ctx, func(l any) bool {
			select {
			case lines <- l:
				return true
			case <-ctx.Done():
				return false
			}
		})
		close(lines)
	}()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	var batch []any
	send := func() bool {
		if len(batch) == 0 {
			return true
		}
		data, _ := json.Marshal(batch)
		batch = batch[:0]
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	// An empty event at once, so the page knows the stream is open even when the log is quiet
	fmt.Fprint(w, ": open\n\n")
	flusher.Flush()
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				send()
				if err := <-done; err != nil {
					fmt.Fprintf(w, "event: failed\ndata: %q\n\n", err.Error())
					flusher.Flush()
				}
				return
			}
			batch = append(batch, l)
			if len(batch) >= 2000 && !send() {
				return
			}
		case <-tick.C:
			if !send() {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

// ---------- files ----------

// mountRoot finds the folder on the server behind one of the container's mounts,
// given as ?mount=<its path inside the container>.
func (a *Agent) mountRoot(r *http.Request) (docker.Details, docker.Mount, string, error) {
	d, err := a.inspect(r)
	if err != nil {
		return d, docker.Mount{}, "", err
	}
	// A mount is named by where it appears inside the container, which is unique.
	// Not by its place in the list: Docker returns mounts in a different order each time
	dest := r.URL.Query().Get("mount")
	i := slices.IndexFunc(d.Mounts, func(m docker.Mount) bool { return m.Destination == dest })
	if i < 0 {
		return d, docker.Mount{}, "", errors.New("no folder " + dest + " in this container")
	}
	m := d.Mounts[i]
	if m.Source == "" {
		return d, m, "", errors.New("this folder has no place on the server")
	}
	return d, m, m.Source, nil
}

func (a *Agent) fileList(w http.ResponseWriter, r *http.Request) {
	_, _, root, err := a.mountRoot(r)
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	entries, truncated, err := files.List(root, r.URL.Query().Get("path"))
	if err != nil {
		fail(w, http.StatusBadRequest, readable(err))
		return
	}
	if entries == nil {
		entries = []files.Entry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "truncated": truncated, "path": files.Clean(r.URL.Query().Get("path"))})
}

// fileRaw sends a file: shown in the page when it can be (text, images, PDF),
// otherwise, or with download=1, as a download.
func (a *Agent) fileRaw(w http.ResponseWriter, r *http.Request) {
	_, _, root, err := a.mountRoot(r)
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	rt, f, info, err := files.Open(root, r.URL.Query().Get("path"))
	if err != nil {
		fail(w, http.StatusBadRequest, readable(err))
		return
	}
	defer rt.Close()
	defer f.Close()
	name := path.Base(files.Clean(r.URL.Query().Get("path")))
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
		w.Header().Set("Content-Type", "application/octet-stream")
	} else {
		// Shown in the page, but never run: a file can't execute script as the agent
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self'; media-src 'self'; style-src 'unsafe-inline'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
	}
	http.ServeContent(w, r, name, info.ModTime(), f)
}

func (a *Agent) fileUsage(w http.ResponseWriter, r *http.Request) {
	_, _, root, err := a.mountRoot(r)
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	n, bytes, more, err := files.Usage(root, r.URL.Query().Get("path"))
	if err != nil {
		fail(w, http.StatusBadRequest, readable(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": n, "bytes": bytes, "more": more})
}

func (a *Agent) fileDelete(w http.ResponseWriter, r *http.Request) {
	d, m, root, err := a.mountRoot(r)
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	rel := files.Clean(r.URL.Query().Get("path"))
	n, bytes, _, _ := files.Usage(root, rel)
	if err := files.Remove(root, rel); err != nil {
		fail(w, http.StatusBadRequest, readable(err))
		return
	}
	audit(r, "deleted %s (%d files, %d bytes) from %s of container %s", path.Join(m.Source, rel), n, bytes, m.Destination, strings.TrimPrefix(d.Name, "/"))
	writeJSON(w, http.StatusOK, map[string]any{"files": n, "bytes": bytes})
}

// fileUpload saves the files of a multipart form into a folder, streaming them
// to disk without holding them in memory.
func (a *Agent) fileUpload(w http.ResponseWriter, r *http.Request) {
	d, m, root, err := a.mountRoot(r)
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	dir := files.Clean(r.URL.Query().Get("path"))
	replace := r.URL.Query().Get("replace") == "1"
	mr, err := r.MultipartReader()
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	var saved []string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			fail(w, http.StatusBadRequest, err)
			return
		}
		if part.FileName() == "" {
			continue
		}
		name := path.Base(part.FileName())
		n, err := files.Save(root, dir, name, replace, part)
		if err != nil {
			fail(w, http.StatusConflict, readable(err))
			return
		}
		audit(r, "uploaded %s (%d bytes) into %s of container %s", path.Join(m.Source, dir, name), n, m.Destination, strings.TrimPrefix(d.Name, "/"))
		saved = append(saved, name)
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": saved})
}

// readable turns file errors into plain words.
func readable(err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return errors.New("not found")
	case errors.Is(err, fs.ErrPermission):
		return errors.New("not allowed: the server keeps this read-only")
	case strings.Contains(err.Error(), "read-only file system"):
		return errors.New("not allowed: this folder is read-only for LinCos (see the README)")
	case strings.Contains(err.Error(), "path escapes"):
		return errors.New("that path leads outside the container's folder")
	}
	return err
}

// ---------- database ----------

type dbRequest struct {
	User     string `json:"user"`
	Password string `json:"password"`
	DB       string `json:"db"`
	Schema   string `json:"schema"`
	Table    string `json:"table"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
	SQL      string `json:"sql"`
	Confirm  bool   `json:"confirm"`
}

func (a *Agent) db(w http.ResponseWriter, r *http.Request) {
	d, err := a.inspect(r)
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	t, ok := database.Detect(d.Config.Image, d.IP(), d.Env)
	if !ok {
		fail(w, http.StatusBadRequest, errors.New("no database recognized in this container"))
		return
	}
	var req dbRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	// A login typed on the page is used for this request only and never kept
	if req.Password != "" {
		t.User, t.Password = req.User, req.Password
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	limit := req.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	name := strings.TrimPrefix(d.Name, "/")
	switch r.PathValue("action") {
	case "databases":
		dbs, err := t.Databases(ctx)
		if err != nil {
			fail(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"databases": dbs, "default": t.Default})
	case "tables":
		tables, err := t.Tables(ctx, req.DB)
		if err != nil {
			fail(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, tables)
	case "rows":
		res, err := t.Rows(ctx, req.DB, req.Schema, req.Table, max(req.Offset, 0), limit)
		if err != nil {
			fail(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	case "query":
		if !database.ReadOnly(req.SQL) && !req.Confirm {
			writeJSON(w, http.StatusConflict, map[string]any{"needsConfirm": true, "error": "this SQL can change data"})
			return
		}
		res, err := t.Query(ctx, req.DB, req.SQL, 1000)
		if err != nil {
			fail(w, http.StatusBadGateway, err)
			return
		}
		if res.Changed {
			audit(r, "ran SQL that changes data in %s of container %s (%d rows): %s", req.DB, name, res.Affected, oneLine(req.SQL, 300))
		}
		writeJSON(w, http.StatusOK, res)
	default:
		fail(w, http.StatusNotFound, errors.New("unknown action"))
	}
}

func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
