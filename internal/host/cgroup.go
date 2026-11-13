package host

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CgroupMemory is the memory a cgroup really holds: memory.current minus its
// inactive file cache, which the kernel drops first when memory gets short.
// Without that, a service that once read big files (containerd unpacking images,
// journald writing logs) looks like it uses hundreds of MB. docker stats counts
// the same way.
func CgroupMemory(dir string) (uint64, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "memory.current"))
	if err != nil {
		return 0, false
	}
	current, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, false
	}
	if f, err := os.Open(filepath.Join(dir, "memory.stat")); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if v, ok := strings.CutPrefix(sc.Text(), "inactive_file "); ok {
				if inactive, err := strconv.ParseUint(v, 10, 64); err == nil && inactive < current {
					current -= inactive
				}
				break
			}
		}
	}
	return current, true
}

// CgroupCPU is a cgroup's CPU time so far in microseconds, from cpu.stat.
func CgroupCPU(dir string) (uint64, bool) {
	f, err := os.Open(filepath.Join(dir, "cpu.stat"))
	if err != nil {
		return 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "usage_usec "); ok {
			usec, err := strconv.ParseUint(v, 10, 64)
			return usec, err == nil
		}
	}
	return 0, false
}
