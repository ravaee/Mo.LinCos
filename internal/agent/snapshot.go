package agent

import (
	"time"

	"github.com/ravaee/Mo.LinCos/internal/docker"
	"github.com/ravaee/Mo.LinCos/internal/host"
)

// Snapshot is everything the UI shows at one moment. Rates (CPU %, bytes per
// second) cover the time since the previous snapshot.
type Snapshot struct {
	Time       time.Time   `json:"time"`
	Host       HostInfo    `json:"host"`
	CPU        CPU         `json:"cpu"`
	Memory     host.Memory `json:"memory"`
	Load       host.Load   `json:"load"`
	Disks      []Disk      `json:"disks"`
	DiskIO     []IO        `json:"diskIO"`
	Net        []IO        `json:"net"`
	Containers []Container `json:"containers"`
	Services   []Service   `json:"services"`
	Processes  []Process   `json:"processes"`
	Findings   []Finding   `json:"findings"`
	Agent      AgentInfo   `json:"agent"`
	Errors     []string    `json:"errors,omitempty"` // sources that could not be read this time
}

type HostInfo struct {
	Name   string  `json:"name"`
	OS     string  `json:"os"`
	Kernel string  `json:"kernel"`
	Cores  int     `json:"cores"`
	Uptime float64 `json:"uptime"` // seconds
}

type CPU struct {
	Total float64   `json:"total"` // 0-100 over all cores
	Cores []float64 `json:"cores"`
}

type Disk struct {
	Mount  string `json:"mount"`
	Device string `json:"device"`
	FS     string `json:"fs"`
	Size   uint64 `json:"size"`
	Used   uint64 `json:"used"`
	Avail  uint64 `json:"avail"`
}

// IO is a network interface or disk: bytes per second in each direction, and the
// totals since boot. For disks In is read and Out is written.
type IO struct {
	Name     string  `json:"name"`
	InRate   float64 `json:"inRate"`
	OutRate  float64 `json:"outRate"`
	InTotal  uint64  `json:"inTotal"`
	OutTotal uint64  `json:"outTotal"`
}

type Container struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Image     string         `json:"image"`
	Project   string         `json:"project,omitempty"`
	Service   string         `json:"service,omitempty"`
	State     string         `json:"state"`
	Status    string         `json:"status"`
	Health    string         `json:"health,omitempty"`
	Restarts  int            `json:"restarts"`
	StartedAt string         `json:"startedAt,omitempty"`
	ExitCode  int            `json:"exitCode"`
	OOMKilled bool           `json:"oomKilled,omitempty"`
	CPU       float64        `json:"cpu"` // % of one core, as top shows it
	Memory    uint64         `json:"memory"`
	MemLimit  uint64         `json:"memLimit"`
	Pids      int            `json:"pids"`
	LogDriver string         `json:"logDriver,omitempty"`
	LogPath   string         `json:"logPath,omitempty"`
	Mounts    []docker.Mount `json:"mounts"`
}

type Service struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Active      string    `json:"active"`
	Sub         string    `json:"sub"`
	Enabled     string    `json:"enabled"`
	PID         int       `json:"pid"`
	Restarts    int       `json:"restarts"`
	Memory      uint64    `json:"memory"`
	CPU         float64   `json:"cpu"` // % of one core
	Since       time.Time `json:"since"`
}

type Process struct {
	PID     int        `json:"pid"`
	PPID    int        `json:"ppid"`
	User    string     `json:"user"`
	Name    string     `json:"name"`
	Cmd     string     `json:"cmd"`
	State   string     `json:"state"`
	CPU     float64    `json:"cpu"` // % of one core, as top shows it
	RSS     uint64     `json:"rss"`
	Threads int        `json:"threads"`
	Owner   host.Owner `json:"owner"`
	// OwnerName is the container's name for container processes
	OwnerName string `json:"ownerName,omitempty"`
}

// AgentInfo is the agent's own cost, so it can be held to its promise of being light.
type AgentInfo struct {
	Version  string  `json:"version"`
	RSS      uint64  `json:"rss"`
	SampleMs float64 `json:"sampleMs"` // how long this snapshot took to take
	Interval float64 `json:"interval"` // seconds between snapshots
}
