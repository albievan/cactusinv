//go:build unix

package hostinfo

import (
	"fmt"
	"syscall"

	"github.com/albievan/cactusinv/internal/ports"
)

func diskUsage(path string) (ports.DiskUsage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return ports.DiskUsage{}, fmt.Errorf("hostinfo.Disk: %w", err)
	}
	bs := uint64(st.Bsize)                                                                             //nolint:gosec,unconvert // the block size is positive; its type differs per OS
	return ports.DiskUsage{FreeBytes: uint64(st.Bavail) * bs, TotalBytes: uint64(st.Blocks) * bs}, nil //nolint:unconvert // types differ per OS
}
