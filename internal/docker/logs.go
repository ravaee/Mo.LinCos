package docker

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Line is one line a container wrote.
type Line struct {
	Time   string `json:"t"` // RFC 3339 with nanoseconds, as Docker stamps it
	Stream string `json:"s"` // "out" or "err"
	Text   string `json:"m"`
}

// LogOptions picks which lines GET /containers/{id}/logs returns.
type LogOptions struct {
	Tail   int    // the last N lines; 0 means all
	Until  string // only lines before this time (RFC 3339), for reading further back
	Follow bool   // keep the stream open and send new lines as they come
}

// Logs reads a container's log through Docker, whatever log driver it uses.
// emit is called for each line, oldest first, until the log ends, emit returns
// false, or ctx ends.
func (c *Client) Logs(ctx context.Context, id string, tty bool, opt LogOptions, emit func(Line) bool) error {
	q := url.Values{"stdout": {"1"}, "stderr": {"1"}, "timestamps": {"1"}}
	if opt.Tail > 0 {
		q.Set("tail", strconv.Itoa(opt.Tail))
	}
	if opt.Until != "" {
		q.Set("until", opt.Until)
	}
	if opt.Follow {
		q.Set("follow", "1")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/"+url.PathEscape(id)+"/logs?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	stream := &http.Client{Transport: c.http.Transport} // no timeout: following keeps it open
	resp, err := stream.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("docker logs: %s %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	if tty {
		return readLines(resp.Body, "out", emit)
	}
	return demux(resp.Body, emit)
}

// demux splits Docker's multiplexed stream: each frame is an 8-byte header
// (stream type, three zero bytes, payload size) followed by the payload.
func demux(r io.Reader, emit func(Line) bool) error {
	br := bufio.NewReaderSize(r, 32<<10)
	var header [8]byte
	partial := map[string]string{} // a line split across frames
	for {
		if _, err := io.ReadFull(br, header[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			return err
		}
		stream := "out"
		if header[0] == 2 {
			stream = "err"
		}
		size := binary.BigEndian.Uint32(header[4:])
		payload := make([]byte, size)
		if _, err := io.ReadFull(br, payload); err != nil {
			return nil
		}
		text := partial[stream] + string(payload)
		for {
			i := strings.IndexByte(text, '\n')
			if i < 0 {
				break
			}
			if !emit(parseLine(text[:i], stream)) {
				return nil
			}
			text = text[i+1:]
		}
		partial[stream] = text
	}
}

func readLines(r io.Reader, stream string, emit func(Line) bool) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		if !emit(parseLine(sc.Text(), stream)) {
			return nil
		}
	}
	return sc.Err()
}

// parseLine splits "2026-11-13T17:00:00.123456789Z message" into time and text.
func parseLine(s, stream string) Line {
	s = strings.TrimSuffix(s, "\r")
	t, text, ok := strings.Cut(s, " ")
	if !ok || len(t) < 20 || t[4] != '-' {
		return Line{Stream: stream, Text: s}
	}
	return Line{Time: t, Stream: stream, Text: text}
}
