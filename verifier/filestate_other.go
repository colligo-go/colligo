//go:build !linux

package verifier

import "os"

func stateOf(info os.FileInfo) fileState {
	return fileState{size: info.Size(), mtime: info.ModTime().UnixNano()}
}
