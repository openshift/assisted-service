package metrics

import (
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"
)

type DiskStatsHelper interface {
	GetDiskUsage(directory string) (usedBytes uint64, freeBytes uint64, err error)
}

type OSDiskStatsHelper struct {
}

//go:generate mockgen -source=disk_stats_helper.go -package=metrics -destination=mock_disk_stats_helper.go
func NewOSDiskStatsHelper() *OSDiskStatsHelper {
	return &OSDiskStatsHelper{}
}

func (c *OSDiskStatsHelper) getUsedBytesInDirectory(directory string) (uint64, error) {
	var totalBytes uint64
	err := filepath.Walk(directory, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !fileInfo.IsDir() {
			totalBytes += uint64(fileInfo.Size())
		}
		return nil
	})
	return totalBytes, err
}

func (c *OSDiskStatsHelper) GetDiskUsage(directory string) (usedBytes uint64, freeBytes uint64, err error) {
	var stat unix.Statfs_t
	err = unix.Statfs(directory, &stat)
	if err != nil {
		return 0, 0, err
	}
	if stat.Blocks == 0 {
		return 0, 0, errors.New("no blocks found when fetching disk usage for directory")
	}
	usedBytes, err = c.getUsedBytesInDirectory(directory)
	if err != nil {
		return 0, 0, err
	}
	freeBytes = stat.Bfree * uint64(stat.Bsize)
	return usedBytes, freeBytes, nil
}
