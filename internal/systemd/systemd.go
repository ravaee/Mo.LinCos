// Package systemd lists services through systemctl. It is read in two parts,
// because `systemctl show '*.service'` scans every unit file on disk and takes
// half a second: Units is the cheap list of states, and Details is asked only
// for named services, when something changed.
package systemd

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Service is one loaded systemd service.
type Service struct {
	Name        string
	Description string
	Active      string // active, inactive, failed, activating, ...
	Sub         string // running, exited, dead, ...

	// From Details
	Enabled  string // enabled, disabled, static, ...
	PID      int
	Restarts int
	Cgroup   string // the service's cgroup, e.g. /system.slice/cron.service
	Since    time.Time
}

// Units lists every loaded service with its state (about 30 ms).
func Units(ctx context.Context) ([]Service, error) {
	out, err := exec.CommandContext(ctx, "systemctl", "list-units", "--type=service", "--all", "--plain", "--no-legend", "--no-pager").Output()
	if err != nil {
		return nil, err
	}
	return ParseUnits(out), nil
}

// ParseUnits reads `systemctl list-units --plain --no-legend`:
// UNIT LOAD ACTIVE SUB DESCRIPTION, the description running to the end of the line.
func ParseUnits(out []byte) []Service {
	var list []Service
	for line := range bytes.Lines(out) {
		f := strings.Fields(string(line))
		if len(f) < 4 || f[1] != "loaded" || !strings.HasSuffix(f[0], ".service") {
			continue
		}
		list = append(list, Service{Name: f[0], Active: f[2], Sub: f[3], Description: strings.Join(f[4:], " ")})
	}
	return list
}

var detailProperties = "Id,UnitFileState,MainPID,NRestarts,ControlGroup,ActiveEnterTimestamp"

// Details reads what list-units leaves out for the named services, by name.
func Details(ctx context.Context, names []string) (map[string]Service, error) {
	if len(names) == 0 {
		return map[string]Service{}, nil
	}
	args := append([]string{"show", "--no-pager", "-p", detailProperties}, names...)
	out, err := exec.CommandContext(ctx, "systemctl", args...).Output()
	if err != nil {
		return nil, err
	}
	return ParseDetails(out), nil
}

// ParseDetails reads `systemctl show` output: blocks of key=value lines, one block
// per unit, separated by an empty line.
func ParseDetails(out []byte) map[string]Service {
	list := map[string]Service{}
	var cur Service
	flush := func() {
		if cur.Name != "" {
			list[cur.Name] = cur
		}
		cur = Service{}
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			flush()
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "Id":
			cur.Name = value
		case "UnitFileState":
			cur.Enabled = value
		case "MainPID":
			cur.PID, _ = strconv.Atoi(value)
		case "NRestarts":
			cur.Restarts, _ = strconv.Atoi(value)
		case "ControlGroup":
			cur.Cgroup = value
		case "ActiveEnterTimestamp":
			// "Fri 2026-09-11 06:23:38 UTC"
			if t, err := time.Parse("Mon 2006-01-02 15:04:05 MST", value); err == nil {
				cur.Since = t
			}
		}
	}
	flush()
	return list
}
