package host

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ClockTicks is USER_HZ, the unit of the CPU times in /proc. It is 100 on every
// Linux the agent supports.
const ClockTicks = 100

// Process is one running program, read from /proc/<pid>.
type Process struct {
	PID   int
	PPID  int
	Name  string // the kernel's short name (comm)
	Cmd   string // the full command line; empty for kernel threads
	State string // R running, S sleeping, D waiting on disk, Z zombie, ...
	UID   int
	// CPUTicks is user + system time so far, in ClockTicks. Usage is the change
	// between two readings.
	CPUTicks uint64
	// StartTicks is when the process started, in ticks after boot. With the PID it
	// tells a process from a later one that reused its PID.
	StartTicks uint64
	RSS        uint64 // resident memory, bytes
	Threads    int
	Owner      Owner
}

// OwnerKind says what started a process.
type OwnerKind string

const (
	OwnerContainer OwnerKind = "container"
	OwnerService   OwnerKind = "service"
	OwnerUser      OwnerKind = "user"   // a login session
	OwnerKernel    OwnerKind = "kernel" // kernel threads
	OwnerSystem    OwnerKind = "system" // systemd itself and anything else
)

// Owner is the container or systemd service a process belongs to, from its cgroup.
type Owner struct {
	Kind OwnerKind `json:"kind"`
	// ID is the container's full ID or the service's unit name.
	ID string `json:"id,omitempty"`
}

// OwnerOf maps a cgroup v2 path, as in /proc/<pid>/cgroup, to its owner:
//
//	/system.slice/docker-<id>.scope  → container <id>
//	/system.slice/cron.service       → service cron.service
//	/user.slice/...                  → a login session
func OwnerOf(cgroup string) Owner {
	parts := strings.Split(strings.Trim(cgroup, "/"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		p := parts[i]
		if id, ok := strings.CutPrefix(p, "docker-"); ok && strings.HasSuffix(id, ".scope") {
			return Owner{Kind: OwnerContainer, ID: strings.TrimSuffix(id, ".scope")}
		}
		// The cgroupfs driver puts containers under /docker/<id>
		if i > 0 && parts[i-1] == "docker" && len(p) == 64 {
			return Owner{Kind: OwnerContainer, ID: p}
		}
	}
	for i := len(parts) - 1; i >= 0; i-- {
		if strings.HasSuffix(parts[i], ".service") && !strings.HasPrefix(parts[i], "user@") {
			return Owner{Kind: OwnerService, ID: parts[i]}
		}
	}
	if len(parts) > 0 && parts[0] == "user.slice" {
		return Owner{Kind: OwnerUser}
	}
	return Owner{Kind: OwnerSystem}
}

// Fixed is what stays the same for a process's whole life: its command line,
// user and cgroup. It is read once per process, not on every round.
type Fixed struct {
	Cmd   string
	UID   int
	Owner Owner
}

// ProcessKey tells a process from a later one that reuses its PID.
type ProcessKey struct {
	PID   int
	Start uint64
}

// ReadProcesses reads every process's /proc/<pid>/stat, and its Fixed part from
// known when there, otherwise from /proc. One that exits while it is being read
// is skipped, not an error. The Fixed parts it used are returned for next time.
func ReadProcesses(proc string, known map[ProcessKey]Fixed) ([]Process, map[ProcessKey]Fixed, error) {
	entries, err := os.ReadDir(proc)
	if err != nil {
		return nil, known, err
	}
	page := uint64(os.Getpagesize())
	out := make([]Process, 0, len(entries))
	next := make(map[ProcessKey]Fixed, len(entries))
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		dir := filepath.Join(proc, e.Name())
		p := Process{PID: pid}
		stat, err := os.ReadFile(filepath.Join(dir, "stat"))
		if err != nil || !parseStat(stat, &p, page) {
			continue
		}
		key := ProcessKey{pid, p.StartTicks}
		f, ok := known[key]
		if !ok {
			f = readFixed(dir, p.PPID, pid)
		}
		next[key] = f
		p.Cmd, p.UID, p.Owner = f.Cmd, f.UID, f.Owner
		out = append(out, p)
	}
	return out, next, nil
}

func readFixed(dir string, ppid, pid int) Fixed {
	var f Fixed
	if cmd, err := os.ReadFile(filepath.Join(dir, "cmdline")); err == nil {
		f.Cmd = strings.TrimSpace(string(bytes.ReplaceAll(cmd, []byte{0}, []byte{' '})))
	}
	f.UID = readUID(filepath.Join(dir, "status"))
	if cg, err := os.ReadFile(filepath.Join(dir, "cgroup")); err == nil {
		// cgroup v2 has the single line "0::<path>"
		for line := range bytes.Lines(cg) {
			if path, ok := bytes.CutPrefix(bytes.TrimSpace(line), []byte("0::")); ok {
				f.Owner = OwnerOf(string(path))
			}
		}
	}
	if f.Cmd == "" && (ppid == 2 || pid == 2) {
		f.Owner = Owner{Kind: OwnerKernel}
	}
	return f
}

// parseStat reads /proc/<pid>/stat. The name sits in parentheses and may itself
// hold spaces or parentheses, so the fields are counted from the last ')'.
func parseStat(data []byte, p *Process, page uint64) bool {
	open := bytes.IndexByte(data, '(')
	end := bytes.LastIndexByte(data, ')')
	if open < 0 || end < open {
		return false
	}
	p.Name = string(data[open+1 : end])
	f := strings.Fields(string(data[end+1:]))
	// f[0] is field 3 (state) of proc(5)
	if len(f) < 22 {
		return false
	}
	num := func(field int) uint64 { v, _ := strconv.ParseUint(f[field-3], 10, 64); return v }
	p.State = f[0]
	p.PPID = int(num(4))
	p.CPUTicks = num(14) + num(15)
	p.Threads = int(num(20))
	p.StartTicks = num(22)
	p.RSS = num(24) * page
	return true
}

func readUID(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return -1
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(sc.Text(), "Uid:"); ok {
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				uid, _ := strconv.Atoi(fields[0])
				return uid
			}
		}
	}
	return -1
}

// Users maps user IDs to names from an /etc/passwd file.
func Users(passwd string) map[int]string {
	out := map[int]string{}
	data, err := os.ReadFile(passwd)
	if err != nil {
		return out
	}
	for line := range bytes.Lines(data) {
		f := strings.Split(strings.TrimSpace(string(line)), ":")
		if len(f) < 3 {
			continue
		}
		if uid, err := strconv.Atoi(f[2]); err == nil {
			out[uid] = f[0]
		}
	}
	return out
}
