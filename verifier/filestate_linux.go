package verifier

import (
	"os"
	"syscall"
)

func stateOf(info os.FileInfo) fileState {
	state := fileState{size: info.Size(), mtime: info.ModTime().UnixNano()}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		state.ctime = st.Ctim.Nano()
		state.inode, state.dev = st.Ino, uint64(st.Dev)
	}
	return state
}
