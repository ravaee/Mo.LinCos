package systemd

import (
	"testing"
	"time"
)

const units = `acpid.service                    loaded    inactive dead    ACPI event daemon
backup.service                   loaded    failed   failed  Nightly backup
chrony.service                   loaded    active   running chrony, an NTP client/server
gone.service                     not-found inactive dead    gone.service
`

func TestParseUnits(t *testing.T) {
	list := ParseUnits([]byte(units))
	if len(list) != 3 {
		t.Fatalf("got %d services, want 3 (the not-found one left out): %+v", len(list), list)
	}
	c := list[2]
	if c.Name != "chrony.service" || c.Active != "active" || c.Sub != "running" || c.Description != "chrony, an NTP client/server" {
		t.Errorf("chrony = %+v", c)
	}
	if list[1].Active != "failed" {
		t.Errorf("backup = %+v", list[1])
	}
}

const details = `Id=chrony.service
UnitFileState=enabled
MainPID=812
NRestarts=0
ControlGroup=/system.slice/chrony.service
ActiveEnterTimestamp=Fri 2026-09-11 06:23:38 UTC

Id=backup.service
UnitFileState=static
MainPID=0
NRestarts=3
ControlGroup=
ActiveEnterTimestamp=
`

func TestParseDetails(t *testing.T) {
	d := ParseDetails([]byte(details))
	c := d["chrony.service"]
	if c.PID != 812 || c.Enabled != "enabled" || c.Cgroup != "/system.slice/chrony.service" {
		t.Errorf("chrony = %+v", c)
	}
	if want := time.Date(2026, 9, 11, 6, 23, 38, 0, time.UTC); !c.Since.Equal(want) {
		t.Errorf("since = %v, want %v", c.Since, want)
	}
	b := d["backup.service"]
	if b.Restarts != 3 || b.Cgroup != "" || !b.Since.IsZero() {
		t.Errorf("backup = %+v", b)
	}
}

func TestParseEntry(t *testing.T) {
	e, ok := parseEntry([]byte(`{"__REALTIME_TIMESTAMP":"1794590221620494","__CURSOR":"s=1;i=2","MESSAGE":"(root) CMD (run-parts)","PRIORITY":"3"}`))
	if !ok || e.Text != "(root) CMD (run-parts)" || e.Stream != "err" || e.Cursor != "s=1;i=2" || e.Time != "2026-11-13T17:17:01.620494Z" {
		t.Errorf("entry = %+v", e)
	}
	// A message that is not valid UTF-8 comes as an array of bytes
	e, ok = parseEntry([]byte(`{"__REALTIME_TIMESTAMP":"1","MESSAGE":[104,105],"PRIORITY":"6"}`))
	if !ok || e.Text != "hi" || e.Stream != "out" {
		t.Errorf("byte message = %+v", e)
	}
}
