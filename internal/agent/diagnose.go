package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Finding is one result of Diagnosis, written as a sentence a person can act on.
type Finding struct {
	Level  string `json:"level"` // ok, info, warn, bad
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	Link   string `json:"link,omitempty"` // a page of the UI, such as #/services/cron.service
}

// extraChecks are slower facts, read every few minutes rather than every round.
type extraChecks struct {
	at             time.Time
	oomKills       int
	lastOOM        string // "sqlservr at 14:02"
	rebootNeeded   bool
	rebootPackages []string
}

const extraEvery = 5 * time.Minute

var killedProcess = regexp.MustCompile(`Killed process \d+ \(([^)]+)\)`)

func (s *Sampler) refreshExtra(ctx context.Context) {
	if time.Since(s.extra.at) < extraEvery {
		return
	}
	e := extraChecks{at: time.Now()}
	out, _ := exec.CommandContext(ctx, "journalctl", "-k", "--since=-24h", "--no-pager", "-o", "short-iso",
		"-g", "Killed process").Output()
	for _, line := range strings.Split(string(out), "\n") {
		if m := killedProcess.FindStringSubmatch(line); m != nil {
			e.oomKills++
			stamp := ""
			if f := strings.Fields(line); len(f) > 0 && len(f[0]) >= 16 {
				stamp = " at " + strings.Replace(f[0][:16], "T", " ", 1)
			}
			e.lastOOM = m[1] + stamp
		}
	}
	if _, err := os.Stat("/var/run/reboot-required"); err == nil {
		e.rebootNeeded = true
		if data, err := os.ReadFile("/var/run/reboot-required.pkgs"); err == nil {
			e.rebootPackages = strings.Fields(string(data))
		}
	}
	s.extra = e
}

// diagnose turns a snapshot into findings: problems first, then what was checked
// and is fine, so the page also says what it looked at.
func diagnose(s Snapshot, e extraChecks, zombies int) []Finding {
	var f []Finding
	add := func(level, title, detail, link string) {
		f = append(f, Finding{Level: level, Title: title, Detail: detail, Link: link})
	}

	// Memory
	m := s.Memory
	if m.Total > 0 {
		free := float64(m.Available) / float64(m.Total) * 100
		title := fmt.Sprintf("Memory: %s of %s free for programs (%.0f%%)", human(m.Available), human(m.Total), free)
		switch {
		case free < 10:
			add("bad", title, "Almost out of memory. Programs may be stopped by Linux. See which use the most under Processes.", "#/processes")
		case free < 20:
			add("warn", title, "Memory is getting short. See which programs use the most under Processes.", "#/processes")
		default:
			add("ok", title, "", "")
		}
	}
	if e.oomKills > 0 {
		add("bad", fmt.Sprintf("Linux stopped %d program(s) for lack of memory in the last 24 hours", e.oomKills),
			"The last one was "+e.lastOOM+". It ran out of memory; give the server more memory or limit that program.", "")
	} else {
		add("ok", "No program was stopped for lack of memory in the last 24 hours", "", "")
	}
	if m.SwapTotal == 0 {
		add("info", "No swap", "If memory ever runs out, Linux stops a program at once instead of slowing down. A swap file of 1–2 GB is a cheap safety net.", "")
	} else if used := m.SwapTotal - m.SwapFree; float64(used) > float64(m.SwapTotal)*0.5 {
		add("warn", fmt.Sprintf("Swap is %s of %s used", human(used), human(m.SwapTotal)), "The server has been short of memory and moved some to disk, which is slow.", "")
	}

	// Disks
	for _, d := range s.Disks {
		if d.Size < 1<<30 {
			continue // small boot partitions
		}
		p := float64(d.Size-d.Avail) / float64(d.Size) * 100
		title := fmt.Sprintf("Disk %s: %s of %s used (%.0f%%)", d.Mount, human(d.Size-d.Avail), human(d.Size), p)
		switch {
		case p >= 90:
			add("bad", title, "Almost full. When it is full, databases and logs stop working.", "")
		case p >= 80:
			add("warn", title, "Getting full.", "")
		default:
			add("ok", title, "", "")
		}
	}

	// CPU
	if s.Host.Cores > 0 {
		per := s.Load.Five / float64(s.Host.Cores)
		title := fmt.Sprintf("CPU: %.0f%% busy now, load %.2f per core over 5 minutes", s.CPU.Total, per)
		switch {
		case per > 1.5:
			add("bad", title, "Much more work than the CPU can do; everything is slower. See Processes.", "#/processes")
		case per > 1:
			add("warn", title, "Work is waiting for the CPU. See Processes.", "#/processes")
		default:
			add("ok", title, "", "")
		}
	}

	// Services
	running, failed := 0, 0
	for _, sv := range s.Services {
		if sv.Sub == "running" {
			running++
		}
		if sv.Active == "failed" {
			failed++
			add("bad", "Service "+strings.TrimSuffix(sv.Name, ".service")+" failed",
				sv.Description+". It is not running; its log says why.", "#/services/"+sv.Name)
		}
	}
	if failed == 0 && len(s.Services) > 0 {
		add("ok", fmt.Sprintf("No failed services (%d running)", running), "", "#/services")
	}

	// Containers
	problems := 0
	for _, c := range s.Containers {
		link := "#/containers/" + c.ID[:12]
		switch {
		case c.State == "restarting":
			add("bad", "Container "+c.Name+" keeps restarting", "It starts and crashes again. Its log says why.", link+"/logs")
		case c.Health == "unhealthy":
			add("bad", "Container "+c.Name+" is unhealthy", "It runs, but its own health check fails.", link)
		case c.OOMKilled:
			add("bad", "Container "+c.Name+" was stopped for lack of memory", "It used more than it may. Raise its memory limit or find what grew.", link)
		case c.State == "exited" && c.ExitCode != 0:
			add("warn", fmt.Sprintf("Container %s stopped with error code %d", c.Name, c.ExitCode), "Its log says why.", link+"/logs")
		case c.State == "running" && c.MemLimit > 0 && float64(c.Memory) > float64(c.MemLimit)*0.9:
			add("warn", fmt.Sprintf("Container %s uses %s of its %s limit", c.Name, human(c.Memory), human(c.MemLimit)), "At the limit it is stopped.", link)
		case c.Restarts > 0:
			add("warn", fmt.Sprintf("Container %s restarted %d time(s)", c.Name, c.Restarts), "Docker started it again after it stopped. Its log says why.", link+"/logs")
		default:
			continue
		}
		problems++
	}
	if problems == 0 && len(s.Containers) > 0 {
		add("ok", fmt.Sprintf("All %d containers are fine", len(s.Containers)), "", "#/containers")
	}

	if e.rebootNeeded {
		detail := "Installed updates take effect after a restart."
		if len(e.rebootPackages) > 0 {
			detail += " Waiting: " + strings.Join(firstN(e.rebootPackages, 6), ", ") + "."
		}
		add("info", "Updates are waiting for a server restart", detail, "")
	}
	if zombies > 0 {
		add("info", fmt.Sprintf("%d finished program(s) not cleaned up (zombies)", zombies), "Harmless in small numbers; their parent program should collect them.", "#/processes")
	}

	order := map[string]int{"bad": 0, "warn": 1, "info": 2, "ok": 3}
	stable(f, func(a, b Finding) bool { return order[a.Level] < order[b.Level] })
	return f
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return append(s[:n:n], fmt.Sprintf("and %d more", len(s)-n))
	}
	return s
}

// stable is an insertion sort that keeps equal findings in the order made.
func stable(f []Finding, less func(a, b Finding) bool) {
	for i := 1; i < len(f); i++ {
		for j := i; j > 0 && less(f[j], f[j-1]); j-- {
			f[j], f[j-1] = f[j-1], f[j]
		}
	}
}

// human writes bytes the way people read them: 1.9 GB, 512 MB.
func human(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	v, i := float64(b), 0
	units := []string{"B", "KB", "MB", "GB", "TB"}
	for v >= unit && i < len(units)-1 {
		v /= unit
		i++
	}
	if v < 10 {
		return fmt.Sprintf("%.1f %s", v, units[i])
	}
	return fmt.Sprintf("%.0f %s", v, units[i])
}
