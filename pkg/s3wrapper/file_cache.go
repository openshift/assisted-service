package s3wrapper

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

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

func (l *fileList) get(key string) *file {
	l.Lock()
	defer l.Unlock()

	f, present := l.files[key]
	if !present {
		f = &file{}
		l.files[key] = f
	}
	return f
}

func (l *fileList) clear() {
	l.Lock()
	defer l.Unlock()

	l.files = make(map[string]*file)
}

func downloadFile(ctx context.Context, objectHandler API, objectName string, cacheDir string, public bool) (string, error) {
	var (
		reader io.ReadCloser
		err    error
		size   int64
	)

	if public {
		reader, size, err = objectHandler.DownloadPublic(ctx, objectName)
	} else {
		reader, size, err = objectHandler.Download(ctx, objectName)
	}
	if err != nil {
		return "", err
	}

	f, err := os.Create(filepath.Join(cacheDir, cacheKey(objectName, public)))
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

func cacheKey(objectName string, public bool) string {
	prefix := "private"
	if public {
		prefix = "public"
	}

	return fmt.Sprintf("%s-%s", prefix, objectName)
}

func GetFile(ctx context.Context, objectHandler API, objectName string, cacheDir string, public bool) (string, error) {
	f := cache.get(cacheKey(objectName, public))
	f.Lock()
	defer f.Unlock()

	//cache miss
	if f.path == "" {
		path, err := downloadFile(ctx, objectHandler, objectName, cacheDir, public)
		if err != nil {
			return "", err
		}
		f.path = path
	}

	return f.path, nil
}

func ClearFileCache() {
	cache.clear()
}
