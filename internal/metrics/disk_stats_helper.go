package metrics

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

type DiskStatsHelper interface {
	GetDiskUsage(directory string) (usedBytes uint64, freeBytes uint64, err error)
}

type OSDiskStatsHelper struct {
	log *logrus.Logger
}

//go:generate mockgen -source=disk_stats_helper.go -package=metrics -destination=mock_disk_stats_helper.go
func NewOSDiskStatsHelper(log *logrus.Logger) *OSDiskStatsHelper {
	return &OSDiskStatsHelper{log: log}
}

func (c *OSDiskStatsHelper) getUsedBytesInDirectory(directory string) (uint64, error) {
	var totalBytes uint64
	// Maintain a map of inodes we have seen so that we don't double count storage
	seenInodes := make(map[uint64]bool)
	err := filepath.Walk(directory, func(path string, fileInfo os.FileInfo, err error) error {
		if err != nil {
			if _, ok := err.(*os.PathError); ok {
				// it's possible to encounter a path error if a file has been deleted since we obtained the file list
				// in cases where deletion occurs outside of Mutex protection, such as when we clean up an installercache.Release, this can be expected
				// we can safely count this as 'zero sized' in these cases
				c.log.WithError(err).Warnf("could not stat file %s because it was deleted before we could walk it - assuming zero size", path)
				// return nil to ignore the error.
				return nil
			}
			return err
		}
		// We need to ensure that the size check is based on inodes and not just the sizes gleaned from files.
		// we should ensure that we have not seen a particular inode for a given file before.
		// this is because there are hard links in use and we need to account for this.
		stat, ok := fileInfo.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("unable to determine stat information for path %s", path)
		}
		if !fileInfo.IsDir() && !seenInodes[stat.Ino] {
			totalBytes += uint64(fileInfo.Size())
			seenInodes[stat.Ino] = true
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
