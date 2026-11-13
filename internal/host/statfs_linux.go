package host

import "syscall"

// Space returns the size of the filesystem at path and how much of it is in use
// and still free for ordinary users (root keeps a reserve on ext4).
func Space(path string) (size, used, avail uint64, err error) {
	var st syscall.Statfs_t
	if err = syscall.Statfs(path, &st); err != nil {
		return
	}
	bs := uint64(st.Bsize)
	size = st.Blocks * bs
	used = (st.Blocks - st.Bfree) * bs
	avail = st.Bavail * bs
	return
}
