package host

import (
	"os"
	"testing"
)

const proc = "testdata/proc"

func TestReadCPU(t *testing.T) {
	all, cores, err := ReadCPU(proc)
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 10000 || all.Idle != 8500 {
		t.Errorf("all = %+v, want Total 10000 Idle 8500", all)
	}
	if len(cores) != 2 {
		t.Fatalf("got %d cores, want 2", len(cores))
	}
	if got := Usage(CPUTimes{Total: 9000, Idle: 8000}, all); got != 50 {
		t.Errorf("Usage = %v, want 50", got)
	}
	if got := Usage(all, all); got != 0 {
		t.Errorf("Usage with no change = %v, want 0", got)
	}
}

func TestReadMemory(t *testing.T) {
	m, err := ReadMemory(proc)
	if err != nil {
		t.Fatal(err)
	}
	if m.Total != 3884000*1024 || m.Available != 1990000*1024 {
		t.Errorf("memory = %+v", m)
	}
	if m.Cached != (50000+2000000+100000)*1024 {
		t.Errorf("cached = %d, want buffers + cached + reclaimable", m.Cached)
	}
}

func TestReadLoadAndUptime(t *testing.T) {
	l, err := ReadLoad(proc)
	if err != nil {
		t.Fatal(err)
	}
	if l.One != 0.52 || l.Fifteen != 0.30 || l.Running != 2 || l.Tasks != 312 {
		t.Errorf("load = %+v", l)
	}
	if up := ReadUptime(proc); up != 5443200.12 {
		t.Errorf("uptime = %v", up)
	}
}

func TestReadNetSkipsVirtualInterfaces(t *testing.T) {
	n, err := ReadNet(proc)
	if err != nil {
		t.Fatal(err)
	}
	if len(n) != 1 || n[0].Name != "eth0" || n[0].Rx != 1000000 || n[0].Tx != 2000000 {
		t.Errorf("net = %+v, want only eth0", n)
	}
}

func TestReadDiskIOWholeDisksOnly(t *testing.T) {
	d, err := ReadDiskIO(proc, "testdata/sys")
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 1 || d[0].Name != "sda" || d[0].Read != 20000*512 || d[0].Write != 40000*512 {
		t.Errorf("disks = %+v, want only sda", d)
	}
}

func TestReadMounts(t *testing.T) {
	m, err := ReadMounts(proc)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 || m[0].Path != "/" || m[1].Path != "/boot/efi" {
		t.Errorf("mounts = %+v, want / and /boot/efi once each", m)
	}
	if got := unescapeMount(`/a\040b`); got != "/a b" {
		t.Errorf("unescape = %q", got)
	}
}

func TestReadProcesses(t *testing.T) {
	ps, fixed, err := ReadProcesses(proc, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A second round takes command, user and owner from the first, not from /proc
	key := ProcessKey{4242, 12345}
	fixed[key] = Fixed{Cmd: "from cache"}
	again, _, _ := ReadProcesses(proc, fixed)
	if len(again) != 1 || again[0].Cmd != "from cache" {
		t.Errorf("second round did not reuse the fixed part: %+v", again)
	}
	if len(ps) != 1 {
		t.Fatalf("got %d processes, want 1", len(ps))
	}
	p := ps[0]
	if p.PID != 4242 || p.Name != "my (odd) name" || p.State != "S" || p.PPID != 1 {
		t.Errorf("process = %+v", p)
	}
	if p.CPUTicks != 200 || p.Threads != 7 || p.StartTicks != 12345 || p.RSS != 300*uint64(os.Getpagesize()) {
		t.Errorf("numbers = ticks %d threads %d start %d rss %d", p.CPUTicks, p.Threads, p.StartTicks, p.RSS)
	}
	if p.Cmd != "dotnet Shop.dll" || p.UID != 1000 {
		t.Errorf("cmd %q uid %d", p.Cmd, p.UID)
	}
	if p.Owner.Kind != OwnerContainer || p.Owner.ID != "0f3a9c2e7b1d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8091a2b3c4d5e6f708192a3b" {
		t.Errorf("owner = %+v", p.Owner)
	}
}

func TestOwnerOf(t *testing.T) {
	id := "0f3a9c2e7b1d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8091a2b3c4d5e6f708192a3b"
	cases := map[string]Owner{
		"/system.slice/docker-" + id + ".scope":              {OwnerContainer, id},
		"/docker/" + id:                                      {OwnerContainer, id},
		"/system.slice/cron.service":                         {OwnerService, "cron.service"},
		"/user.slice/user-0.slice/session-5.scope":           {Kind: OwnerUser},
		"/user.slice/user-0.slice/user@0.service/init.scope": {Kind: OwnerUser},
		"/init.scope":                                        {Kind: OwnerSystem},
		"/":                                                  {Kind: OwnerSystem},
	}
	for path, want := range cases {
		if got := OwnerOf(path); got != want {
			t.Errorf("OwnerOf(%q) = %+v, want %+v", path, got, want)
		}
	}
}
