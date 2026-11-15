//go:build !unix

package hostinfo

import (
	"fmt"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

func diskUsage(string) (ports.DiskUsage, error) {
	return ports.DiskUsage{}, fmt.Errorf("hostinfo.Disk: %w: free space on this system", domain.ErrNotConfigured)
}
