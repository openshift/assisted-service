package isoutil

import (
	"io"
	"os"

	diskfs "github.com/diskfs/go-diskfs"
)

//go:generate mockgen -package=isoutil -destination=mock_isoutil.go . Handler
type Handler interface {
	ReadFile(filePath string) (io.ReadWriteSeeker, error)
}

type installerHandler struct {
	isoPath string
	workDir string
}

func NewHandler(isoPath, workDir string) Handler {
	return &installerHandler{isoPath: isoPath, workDir: workDir}
}

func (h *installerHandler) ReadFile(filePath string) (io.ReadWriteSeeker, error) {
	d, err := diskfs.OpenWithMode(h.isoPath, diskfs.ReadOnly)
	if err != nil {
		return nil, err
	}

	fs, err := d.GetFilesystem(0)
	if err != nil {
		return nil, err
	}

	fsFile, err := fs.OpenFile(filePath, os.O_RDONLY)
	if err != nil {
		return nil, err
	}

	return fsFile, nil
}
