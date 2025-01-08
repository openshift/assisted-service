package installercache

import (
	"golang.org/x/sys/unix"
)

//go:generate mockgen -source=unix_helper.go -package=installercache -destination=mock_unix_helper.go
type UnixHelper interface {
	Statfs(path string, buf *unix.Statfs_t) (err error)
}

type OSUnixHelper struct {
}

func NewOSUnixHelper() *OSUnixHelper {
	return &OSUnixHelper{}
}

func (h *OSUnixHelper) Statfs(path string, buf *unix.Statfs_t) (err error) {
	return unix.Statfs(path, buf)
}
