package systemd

import (
	"bufio"
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"time"
)

// Entry is one line of the journal.
type Entry struct {
	Time   string `json:"t"` // RFC 3339
	Stream string `json:"s"` // "err" for errors and worse, "warn", or "out"
	Text   string `json:"m"`
	Cursor string `json:"c"` // where it is in the journal, to read further back from
}

// JournalOptions picks which lines Journal returns.
type JournalOptions struct {
	Unit   string // a service such as cron.service; empty means the whole system
	Lines  int    // the last N lines
	Before string // only lines before this cursor, for reading further back
	Follow bool   // keep going with new lines as they come
}

// Journal reads the journal with journalctl, oldest line first. emit is called
// for each line until the output ends, emit returns false or ctx ends.
func Journal(ctx context.Context, opt JournalOptions, emit func(Entry) bool) error {
	args := []string{"--no-pager", "-o", "json", "--output-fields=MESSAGE,PRIORITY"}
	if opt.Unit != "" {
		args = append(args, "-u", opt.Unit)
	}
	lines := opt.Lines
	if lines <= 0 {
		lines = 500
	}
	if opt.Before != "" {
		// Backwards from the cursor, which is itself included and so skipped below
		args = append(args, "-r", "--cursor="+opt.Before, "-n", strconv.Itoa(lines+1))
	} else {
		args = append(args, "-n", strconv.Itoa(lines))
	}
	if opt.Follow {
		args = append(args, "-f")
	}
	cmd := exec.CommandContext(ctx, "journalctl", args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	defer cmd.Wait()
	defer cmd.Cancel()

	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	var back []Entry // backwards reads are collected, then given oldest first
	for sc.Scan() {
		e, ok := parseEntry(sc.Bytes())
		if !ok {
			continue
		}
		if opt.Before != "" {
			if e.Cursor != opt.Before {
				back = append(back, e)
			}
			continue
		}
		if !emit(e) {
			return nil
		}
	}
	for i := len(back) - 1; i >= 0; i-- {
		if !emit(back[i]) {
			return nil
		}
	}
	return nil
}

func parseEntry(line []byte) (Entry, bool) {
	var raw struct {
		Time     string          `json:"__REALTIME_TIMESTAMP"`
		Cursor   string          `json:"__CURSOR"`
		Message  json.RawMessage `json:"MESSAGE"`
		Priority string          `json:"PRIORITY"`
	}
	if json.Unmarshal(line, &raw) != nil {
		return Entry{}, false
	}
	e := Entry{Cursor: raw.Cursor, Stream: "out"}
	if us, err := strconv.ParseInt(raw.Time, 10, 64); err == nil {
		e.Time = time.UnixMicro(us).UTC().Format(time.RFC3339Nano)
	}
	// MESSAGE is a string, or an array of bytes when it is not valid UTF-8
	if json.Unmarshal(raw.Message, &e.Text) != nil {
		var b []byte
		var nums []int
		if json.Unmarshal(raw.Message, &nums) == nil {
			for _, n := range nums {
				b = append(b, byte(n))
			}
		}
		e.Text = string(b)
	}
	switch raw.Priority {
	case "0", "1", "2", "3":
		e.Stream = "err"
	case "4":
		e.Stream = "warn"
	}
	return e, true
}
