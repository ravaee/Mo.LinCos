package agent

import (
	"bufio"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func rate(prev, cur uint64, seconds float64) float64 {
	if cur < prev || seconds <= 0 {
		return 0 // the counter wrapped or was reset
	}
	return math.Round(float64(cur-prev) / seconds)
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

func itoa(v int) string { return strconv.Itoa(v) }

func readTrim(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// osName is PRETTY_NAME from os-release, e.g. "Ubuntu 26.04 LTS".
func osName(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

// selfRSS is the agent's own resident memory from /proc/self/status.
func selfRSS(proc string) uint64 {
	f, err := os.Open(filepath.Join(proc, "self", "status"))
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "VmRSS:"); ok {
			fields := strings.Fields(v)
			if len(fields) > 0 {
				kb, _ := strconv.ParseUint(fields[0], 10, 64)
				return kb * 1024
			}
		}
	}
	return 0
}
