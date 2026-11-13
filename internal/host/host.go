// Package host reads the machine's own numbers from /proc and /sys: CPU, memory,
// load, network, disks and processes. Every reader takes the proc root as a
// parameter, so tests run against copies of real files in testdata.
package host

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CPUTimes are the kernel's cumulative counters for one CPU line of /proc/stat,
// in clock ticks. Usage is the change between two readings.
type CPUTimes struct {
	Total uint64
	Idle  uint64
}

// ReadCPU returns the counters for all CPUs together and for each core.
func ReadCPU(proc string) (all CPUTimes, cores []CPUTimes, err error) {
	data, err := os.ReadFile(filepath.Join(proc, "stat"))
	if err != nil {
		return all, nil, err
	}
	for line := range bytes.Lines(data) {
		if !bytes.HasPrefix(line, []byte("cpu")) {
			break // the cpu lines come first
		}
		fields := strings.Fields(string(line))
		var t CPUTimes
		// user nice system idle iowait irq softirq steal; guest time is already inside user
		for i := 1; i < len(fields) && i <= 8; i++ {
			v, _ := strconv.ParseUint(fields[i], 10, 64)
			t.Total += v
			if i == 4 || i == 5 { // idle, iowait
				t.Idle += v
			}
		}
		if fields[0] == "cpu" {
			all = t
		} else {
			cores = append(cores, t)
		}
	}
	return all, cores, nil
}

// Usage is the share of time busy between two readings, 0-100.
func Usage(prev, cur CPUTimes) float64 {
	total := float64(cur.Total - prev.Total)
	if cur.Total <= prev.Total || total == 0 {
		return 0
	}
	busy := total - float64(cur.Idle-prev.Idle)
	return clamp(busy / total * 100)
}

// Memory is /proc/meminfo in bytes. Available is what programs can still get,
// including cache the kernel gives back on demand.
type Memory struct {
	Total     uint64 `json:"total"`
	Available uint64 `json:"available"`
	Free      uint64 `json:"free"`
	Cached    uint64 `json:"cached"` // page cache + buffers + reclaimable slab
	SwapTotal uint64 `json:"swapTotal"`
	SwapFree  uint64 `json:"swapFree"`
}

func ReadMemory(proc string) (Memory, error) {
	var m Memory
	f, err := os.Open(filepath.Join(proc, "meminfo"))
	if err != nil {
		return m, err
	}
	defer f.Close()
	var buffers, cached, sReclaimable uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		v, _ := strconv.ParseUint(fields[0], 10, 64)
		v *= 1024 // kB
		switch key {
		case "MemTotal":
			m.Total = v
		case "MemAvailable":
			m.Available = v
		case "MemFree":
			m.Free = v
		case "Buffers":
			buffers = v
		case "Cached":
			cached = v
		case "SReclaimable":
			sReclaimable = v
		case "SwapTotal":
			m.SwapTotal = v
		case "SwapFree":
			m.SwapFree = v
		}
	}
	m.Cached = buffers + cached + sReclaimable
	return m, sc.Err()
}

// Load is /proc/loadavg: run-queue averages and the number of tasks.
type Load struct {
	One     float64 `json:"one"`
	Five    float64 `json:"five"`
	Fifteen float64 `json:"fifteen"`
	Running int     `json:"running"`
	Tasks   int     `json:"tasks"`
}

func ReadLoad(proc string) (Load, error) {
	var l Load
	data, err := os.ReadFile(filepath.Join(proc, "loadavg"))
	if err != nil {
		return l, err
	}
	f := strings.Fields(string(data))
	if len(f) < 4 {
		return l, nil
	}
	l.One, _ = strconv.ParseFloat(f[0], 64)
	l.Five, _ = strconv.ParseFloat(f[1], 64)
	l.Fifteen, _ = strconv.ParseFloat(f[2], 64)
	running, tasks, _ := strings.Cut(f[3], "/")
	l.Running, _ = strconv.Atoi(running)
	l.Tasks, _ = strconv.Atoi(tasks)
	return l, nil
}

// ReadUptime returns the seconds since boot.
func ReadUptime(proc string) float64 {
	data, err := os.ReadFile(filepath.Join(proc, "uptime"))
	if err != nil {
		return 0
	}
	first, _, _ := strings.Cut(string(data), " ")
	v, _ := strconv.ParseFloat(first, 64)
	return v
}

// NetCounters are the cumulative bytes of one interface in /proc/net/dev.
type NetCounters struct {
	Name string
	Rx   uint64
	Tx   uint64
}

// ReadNet returns the host's own interfaces. Loopback and the virtual ends of
// container networks are left out: their traffic is counted again elsewhere.
func ReadNet(proc string) ([]NetCounters, error) {
	data, err := os.ReadFile(filepath.Join(proc, "net", "dev"))
	if err != nil {
		return nil, err
	}
	var out []NetCounters
	for line := range bytes.Lines(data) {
		name, rest, ok := strings.Cut(string(line), ":")
		if !ok {
			continue // the two header lines
		}
		name = strings.TrimSpace(name)
		if name == "lo" || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "br-") || name == "docker0" {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(f[0], 10, 64)
		tx, _ := strconv.ParseUint(f[8], 10, 64)
		out = append(out, NetCounters{Name: name, Rx: rx, Tx: tx})
	}
	return out, nil
}

// DiskCounters are the cumulative bytes read and written by one whole disk.
type DiskCounters struct {
	Name  string
	Read  uint64
	Write uint64
}

// ReadDiskIO returns whole disks only (sda, nvme0n1, vda), not their partitions,
// so nothing is counted twice. sys is the /sys root, where whole disks are listed.
func ReadDiskIO(proc, sys string) ([]DiskCounters, error) {
	data, err := os.ReadFile(filepath.Join(proc, "diskstats"))
	if err != nil {
		return nil, err
	}
	var out []DiskCounters
	for line := range bytes.Lines(data) {
		f := strings.Fields(string(line))
		if len(f) < 10 {
			continue
		}
		name := f[2]
		// loop devices, RAM disks and optical drives are not storage worth watching
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "sr") {
			continue
		}
		if _, err := os.Stat(filepath.Join(sys, "block", name)); err != nil {
			continue // a partition
		}
		rd, _ := strconv.ParseUint(f[5], 10, 64)
		wr, _ := strconv.ParseUint(f[9], 10, 64)
		out = append(out, DiskCounters{Name: name, Read: rd * 512, Write: wr * 512}) // sectors are always 512 bytes here
	}
	return out, nil
}

// Mount is one real filesystem from the mount table.
type Mount struct {
	Device string
	Path   string
	FS     string
}

// realFS are the filesystem types that hold data on a disk; the rest of the
// mount table is kernel and container plumbing.
var realFS = map[string]bool{"ext4": true, "ext3": true, "ext2": true, "xfs": true, "btrfs": true, "zfs": true, "vfat": true, "f2fs": true}

// ReadMounts returns each real filesystem once, at its shortest mount path.
func ReadMounts(proc string) ([]Mount, error) {
	data, err := os.ReadFile(filepath.Join(proc, "self", "mounts"))
	if err != nil {
		return nil, err
	}
	seen := map[string]int{}
	var out []Mount
	for line := range bytes.Lines(data) {
		f := strings.Fields(string(line))
		if len(f) < 3 || !realFS[f[2]] {
			continue
		}
		m := Mount{Device: f[0], Path: unescapeMount(f[1]), FS: f[2]}
		if i, ok := seen[m.Device]; ok {
			if len(m.Path) < len(out[i].Path) {
				out[i] = m
			}
			continue
		}
		seen[m.Device] = len(out)
		out = append(out, m)
	}
	return out, nil
}

// unescapeMount undoes the octal escapes /proc/mounts uses for spaces and tabs.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
