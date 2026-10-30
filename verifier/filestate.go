package verifier

import (
	"errors"
	"fmt"
	"os"
)

type fileState struct {
	size  int64
	mtime int64
	ctime int64
	inode uint64
	dev   uint64
}

func (a fileState) differences(b fileState) []string {
	var changed []string
	if a.size != b.size {
		changed = append(changed, "size")
	}
	if a.mtime != b.mtime {
		changed = append(changed, "modification time")
	}
	if a.ctime != b.ctime {
		changed = append(changed, "change time")
	}
	if a.inode != b.inode || a.dev != b.dev {
		changed = append(changed, "inode")
	}
	return changed
}

func statFailure(err error) error {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return fmt.Errorf("stat: %w", pe.Err)
	}
	return err
}
