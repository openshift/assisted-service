package s3wrapper

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// list of files keyed by the s3 object id
type fileList struct {
	sync.Mutex
	files map[string]*file
}

type file struct {
	sync.Mutex
	path string
}

var cache fileList = fileList{
	files: make(map[string]*file),
}

func (l *fileList) get(objectName string) *file {
	l.Lock()
	defer l.Unlock()

	f, present := l.files[objectName]
	if !present {
		f = &file{}
		l.files[objectName] = f
	}
	return f
}

func downloadFile(ctx context.Context, objectHandler API, objectName string, cacheDir string) (string, error) {
	reader, size, err := objectHandler.Download(ctx, objectName)
	if err != nil {
		return "", err
	}

	f, err := os.Create(filepath.Join(cacheDir, objectName))
	if err != nil {
		return "", err
	}

	written, err := io.Copy(f, reader)
	if err != nil {
		return "", err
	}
	if written != size {
		return "", fmt.Errorf("failed to write file, expected %d bytes, wrote %d", size, written)
	}

	return f.Name(), nil
}

func GetFile(ctx context.Context, objectHandler API, objectName string, cacheDir string) (string, error) {
	f := cache.get(objectName)
	f.Lock()
	defer f.Unlock()

	//cache miss
	if f.path == "" {
		path, err := downloadFile(ctx, objectHandler, objectName, cacheDir)
		if err != nil {
			return "", err
		}
		f.path = path
	}

	return f.path, nil
}
