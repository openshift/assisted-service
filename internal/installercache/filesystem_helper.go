package installercache

import (
	"errors"

	"golang.org/x/sys/unix"
)

//go:generate mockgen -source=filesystem_helper.go -package=installercache -destination=mock_filesystem_helper.go
type FileSystemHelper interface {
	GetDiskUsageForDirectory(directory string) (uint64, float64, error)
}

type OSFileSystemHelper struct {
	UnixHelper UnixHelper
}

func NewOSFileSystemnHelper(unixHelper UnixHelper) *OSFileSystemHelper {
	return &OSFileSystemHelper{UnixHelper: unixHelper}
}

func (h *OSFileSystemHelper) GetDiskUsageForDirectory(directory string) (uint64, float64, error) {
	var stat unix.Statfs_t
	err := h.UnixHelper.Statfs(directory, &stat)
	if err != nil {
		return 0, 0, err
	}
	if stat.Blocks == 0 {
		return 0, 0, errors.New("no blocks found when fetching disk usage for directory")
	}
	usedBytes := (stat.Blocks - stat.Bfree) * uint64(stat.Bsize)
	usedPercentage := (float64(stat.Blocks-stat.Bfree) / float64(stat.Blocks)) * 100
	return usedBytes, usedPercentage, nil
}
