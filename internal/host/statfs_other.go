//go:build !linux

package host

import "errors"

// Space is Linux-only; the agent only runs there. This lets the rest build and
// test on a Mac.
func Space(string) (size, used, avail uint64, err error) {
	return 0, 0, 0, errors.New("disk space is only read on Linux")
}
