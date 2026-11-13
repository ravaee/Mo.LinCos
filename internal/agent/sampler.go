package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ravaee/Mo.LinCos/internal/docker"
	"github.com/ravaee/Mo.LinCos/internal/host"
	"github.com/ravaee/Mo.LinCos/internal/systemd"
)

// Paths says where the agent reads the machine from. The defaults are the real
// ones; tests point them at copies.
type Paths struct {
	Proc         string
	Sys          string
	Cgroup       string
	DockerSocket string
	Passwd       string
}

func DefaultPaths() Paths {
	return Paths{Proc: "/proc", Sys: "/sys", Cgroup: "/sys/fs/cgroup", DockerSocket: "/var/run/docker.sock", Passwd: "/etc/passwd"}
}

// maxProcesses is how many processes a snapshot carries: the busiest by CPU and
// by memory. A server has hundreds of mostly idle ones.
const maxProcesses = 60

// Sampler takes snapshots. It keeps the previous counters, because every rate is
// the change since then.
type Sampler struct {
	paths   Paths
	docker  *docker.Client
	version string
	info    HostInfo

	last     time.Time
	cpu      host.CPUTimes
	cores    []host.CPUTimes
	procs    map[host.ProcessKey]uint64 // CPU ticks per process
	fixed    map[host.ProcessKey]host.Fixed
	ctrs     map[string]uint64 // CPU microseconds per container
	svcs     map[string]uint64 // CPU microseconds per service
	net      map[string]host.NetCounters
	disks    map[string]host.DiskCounters
	users    map[int]string
	inspects map[string]inspected

	// The container list is read again only when Docker reports a change (see
	// Watch) or once a minute; the cached one serves every round in between
	containerList []docker.Summary
	listedAt      time.Time
	listDirty     atomic.Bool

	extra   extraChecks
	zombies int

	// systemctl is the most expensive read, so the list of services is refreshed
	// less often than the rest and their details only when a state changes. CPU
	// and memory come from their cgroups on every round
	units     []systemd.Service
	unitsAt   time.Time
	details   map[string]systemd.Service
	detailsAt time.Time
	unitsSig  string
}

// inspected is a cached `docker inspect`; it only changes when a container restarts.
type inspected struct {
	details docker.Details
	status  string
}

func NewSampler(paths Paths, version string) *Sampler {
	s := &Sampler{
		paths:    paths,
		docker:   docker.New(paths.DockerSocket),
		version:  version,
		procs:    map[host.ProcessKey]uint64{},
		ctrs:     map[string]uint64{},
		svcs:     map[string]uint64{},
		net:      map[string]host.NetCounters{},
		disks:    map[string]host.DiskCounters{},
		users:    host.Users(paths.Passwd),
		inspects: map[string]inspected{},
	}
	s.info = HostInfo{Cores: runtime.NumCPU()}
	s.info.Name, _ = os.Hostname()
	s.info.Kernel = readTrim(filepath.Join(paths.Proc, "sys", "kernel", "osrelease"))
	s.info.OS = osName("/etc/os-release")
	return s
}

// Take reads everything once. The first call after a long pause has rates over
// that whole pause; the caller decides whether to take a second, closer one.
func (s *Sampler) Take(ctx context.Context) Snapshot {
	start := time.Now()
	snap := Snapshot{Time: start, Host: s.info}
	elapsed := start.Sub(s.last).Seconds()
	first := s.last.IsZero()
	if first {
		elapsed = 0
	}
	fail := func(source string, err error) {
		snap.Errors = append(snap.Errors, source+": "+err.Error())
	}

	snap.Host.Uptime = host.ReadUptime(s.paths.Proc)

	// CPU
	if all, cores, err := host.ReadCPU(s.paths.Proc); err == nil {
		if !first {
			snap.CPU.Total = round1(host.Usage(s.cpu, all))
			snap.CPU.Cores = make([]float64, len(cores))
			for i := range cores {
				if i < len(s.cores) {
					snap.CPU.Cores[i] = round1(host.Usage(s.cores[i], cores[i]))
				}
			}
		}
		s.cpu, s.cores = all, cores
	} else {
		fail("cpu", err)
	}

	var err error
	if snap.Memory, err = host.ReadMemory(s.paths.Proc); err != nil {
		fail("memory", err)
	}
	if snap.Load, err = host.ReadLoad(s.paths.Proc); err != nil {
		fail("load", err)
	}

	// Network and disk throughput
	if ifaces, err := host.ReadNet(s.paths.Proc); err == nil {
		next := map[string]host.NetCounters{}
		for _, n := range ifaces {
			io := IO{Name: n.Name, InTotal: n.Rx, OutTotal: n.Tx}
			if p, ok := s.net[n.Name]; ok && elapsed > 0 {
				io.InRate, io.OutRate = rate(p.Rx, n.Rx, elapsed), rate(p.Tx, n.Tx, elapsed)
			}
			snap.Net = append(snap.Net, io)
			next[n.Name] = n
		}
		s.net = next
	} else {
		fail("network", err)
	}
	if disks, err := host.ReadDiskIO(s.paths.Proc, s.paths.Sys); err == nil {
		next := map[string]host.DiskCounters{}
		for _, d := range disks {
			io := IO{Name: d.Name, InTotal: d.Read, OutTotal: d.Write}
			if p, ok := s.disks[d.Name]; ok && elapsed > 0 {
				io.InRate, io.OutRate = rate(p.Read, d.Read, elapsed), rate(p.Write, d.Write, elapsed)
			}
			snap.DiskIO = append(snap.DiskIO, io)
			next[d.Name] = d
		}
		s.disks = next
	} else {
		fail("disk io", err)
	}
	if mounts, err := host.ReadMounts(s.paths.Proc); err == nil {
		for _, m := range mounts {
			size, used, avail, err := host.Space(m.Path)
			if err != nil {
				continue
			}
			snap.Disks = append(snap.Disks, Disk{Mount: m.Path, Device: m.Device, FS: m.FS, Size: size, Used: used, Avail: avail})
		}
	} else {
		fail("mounts", err)
	}

	names := s.containers(ctx, &snap, elapsed, fail)
	s.processes(&snap, elapsed, names, fail)
	s.servicesInto(ctx, &snap, elapsed, fail)
	s.refreshExtra(ctx)
	snap.Findings = diagnose(snap, s.extra, s.zombies)

	snap.Agent = AgentInfo{
		Version:  s.version,
		RSS:      selfRSS(s.paths.Proc),
		SampleMs: round1(float64(time.Since(start).Microseconds()) / 1000),
	}
	s.last = start
	return snap
}

const listEvery = time.Minute

// WatchDocker follows Docker's events until ctx ends, so the container list is
// read again as soon as something changes.
func (s *Sampler) WatchDocker(ctx context.Context) {
	s.docker.Watch(ctx, func() { s.listDirty.Store(true) })
}

// containers fills in Docker's containers and returns their names by ID, so
// processes can say which container they belong to.
func (s *Sampler) containers(ctx context.Context, snap *Snapshot, elapsed float64, fail func(string, error)) map[string]string {
	names := map[string]string{}
	changed := s.listDirty.Swap(false)
	if changed || time.Since(s.listedAt) >= listEvery {
		if changed {
			// Docker reported a change: a restart can leave the status line looking
			// the same ("Up ..."), so every container's details are read again
			s.inspects = map[string]inspected{}
		}
		list, err := s.docker.List(ctx)
		if err != nil {
			s.listDirty.Store(true)              // try again next round
			if !errors.Is(err, os.ErrNotExist) { // no Docker on this machine is not an error
				fail("docker", err)
			}
			return names
		}
		s.containerList, s.listedAt = list, time.Now()
	}
	list := s.containerList
	nextCPU := map[string]uint64{}
	nextInspect := map[string]inspected{}
	for _, c := range list {
		names[c.ID] = c.Name()
		ins, ok := s.inspects[c.ID]
		// Inspect again only when the container's state line changed in a way
		// that matters (restarted, stopped, health changed), not as "Up 5 minutes" ticks on
		if !ok || stateKey(ins.status) != stateKey(c.Status) {
			if d, err := s.docker.Inspect(ctx, c.ID); err == nil {
				ins = inspected{details: d, status: c.Status}
			} else {
				fail("docker inspect "+c.Name(), err)
			}
		}
		nextInspect[c.ID] = ins
		d := ins.details
		ct := Container{
			ID: c.ID, Name: c.Name(), Image: c.Image, Project: c.Project(), Service: c.Service(),
			State: c.State, Status: c.Status, Restarts: d.RestartCount, ExitCode: d.State.ExitCode,
			LogDriver: d.HostConfig.LogConfig.Type, LogPath: d.LogPath, Mounts: d.Mounts, OOMKilled: d.State.OOMKilled,
		}
		if c.State == "running" {
			ct.StartedAt = d.State.StartedAt
		}
		if d.State.Health != nil {
			ct.Health = d.State.Health.Status
		}
		if ct.Mounts == nil {
			ct.Mounts = []docker.Mount{}
		}
		ct.Mounts = slices.Clone(ct.Mounts) // the cached details stay as they are
		docker.SortMounts(ct.Mounts)
		if u, ok := docker.ReadUsage(s.paths.Cgroup, c.ID); ok {
			ct.Memory, ct.MemLimit, ct.Pids = u.Memory, u.Limit, u.Pids
			if p, ok := s.ctrs[c.ID]; ok && elapsed > 0 && u.CPUMicros >= p {
				ct.CPU = round1(float64(u.CPUMicros-p) / 1e6 / elapsed * 100)
			}
			nextCPU[c.ID] = u.CPUMicros
		}
		snap.Containers = append(snap.Containers, ct)
	}
	s.ctrs, s.inspects = nextCPU, nextInspect
	sort.Slice(snap.Containers, func(i, j int) bool {
		a, b := snap.Containers[i], snap.Containers[j]
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		return a.Name < b.Name
	})
	return names
}

// stateKey is the part of a container's status line that changes on events:
// "Up 7 days (healthy)" → "Up (healthy)".
func stateKey(status string) string {
	word, _, _ := strings.Cut(status, " ")
	if i := strings.Index(status, "("); i >= 0 {
		return word + " " + status[i:]
	}
	return word
}

func (s *Sampler) processes(snap *Snapshot, elapsed float64, containerNames map[string]string, fail func(string, error)) {
	list, fixed, err := host.ReadProcesses(s.paths.Proc, s.fixed)
	if err != nil {
		fail("processes", err)
		return
	}
	s.fixed = fixed
	next := make(map[host.ProcessKey]uint64, len(list))
	all := make([]Process, 0, len(list))
	s.zombies = 0
	for _, p := range list {
		if p.State == "Z" {
			s.zombies++
		}
		key := host.ProcessKey{PID: p.PID, Start: p.StartTicks}
		pr := Process{
			PID: p.PID, PPID: p.PPID, Name: p.Name, Cmd: p.Cmd, State: p.State,
			RSS: p.RSS, Threads: p.Threads, Owner: p.Owner, User: s.userName(p.UID),
		}
		if prev, ok := s.procs[key]; ok && elapsed > 0 && p.CPUTicks >= prev {
			pr.CPU = round1(float64(p.CPUTicks-prev) / host.ClockTicks / elapsed * 100)
		}
		if p.Owner.Kind == host.OwnerContainer {
			pr.OwnerName = containerNames[p.Owner.ID]
		}
		next[key] = p.CPUTicks
		all = append(all, pr)
	}
	s.procs = next
	snap.Processes = busiest(all, maxProcesses)
}

// busiest keeps the processes using the most CPU and the most memory, half each,
// so a memory hog that is idle right now still shows.
func busiest(all []Process, n int) []Process {
	if len(all) <= n {
		return all
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CPU > all[j].CPU })
	keep := map[int]bool{}
	var out []Process
	for _, p := range all[:n/2] {
		keep[p.PID] = true
		out = append(out, p)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].RSS > all[j].RSS })
	for _, p := range all {
		if len(out) >= n {
			break
		}
		if !keep[p.PID] {
			out = append(out, p)
		}
	}
	return out
}

const (
	unitsEvery   = 5 * time.Second
	detailsEvery = time.Minute
)

func (s *Sampler) servicesInto(ctx context.Context, snap *Snapshot, elapsed float64, fail func(string, error)) {
	if time.Since(s.unitsAt) >= unitsEvery {
		if units, err := systemd.Units(ctx); err != nil {
			fail("systemd", err)
		} else {
			s.units, s.unitsAt = units, time.Now()
			// Details for every service that is not simply stopped, read again when
			// any service changed state, and once a minute for restart counts
			var sig strings.Builder
			var names []string
			for _, u := range units {
				sig.WriteString(u.Name + u.Active + u.Sub + ";")
				if u.Active != "inactive" {
					names = append(names, u.Name)
				}
			}
			if sig.String() != s.unitsSig || time.Since(s.detailsAt) >= detailsEvery {
				if d, err := systemd.Details(ctx, names); err != nil {
					fail("systemd details", err)
				} else {
					s.details, s.detailsAt, s.unitsSig = d, time.Now(), sig.String()
				}
			}
		}
	}
	next := map[string]uint64{}
	for _, u := range s.units {
		d := s.details[u.Name]
		out := Service{
			Name: u.Name, Description: u.Description, Active: u.Active, Sub: u.Sub, Enabled: d.Enabled,
			PID: d.PID, Restarts: d.Restarts, Since: d.Since,
		}
		if d.Cgroup != "" && u.Active != "inactive" {
			dir := filepath.Join(s.paths.Cgroup, d.Cgroup)
			out.Memory, _ = host.CgroupMemory(dir)
			if usec, ok := host.CgroupCPU(dir); ok {
				if p, ok := s.svcs[u.Name]; ok && elapsed > 0 && usec >= p {
					out.CPU = round1(float64(usec-p) / 1e6 / elapsed * 100)
				}
				next[u.Name] = usec
			}
		}
		snap.Services = append(snap.Services, out)
	}
	s.svcs = next
}

func (s *Sampler) userName(uid int) string {
	if name, ok := s.users[uid]; ok {
		return name
	}
	if uid < 0 {
		return "?"
	}
	return itoa(uid)
}
