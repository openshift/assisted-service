package s3wrapper

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

func NewFilesystemBasedXattrClient(
	log logrus.FieldLogger,
	rootDirectory string,
) *FilesystemBasedXattrClient {
	return &FilesystemBasedXattrClient{
		log:           log,
		rootDirectory: rootDirectory,
	}
}

const (
	filesystemXattrMetaDataDirectoryName = "metadata"
	delimiter                            = "@@"
)

type FilesystemBasedXattrClient struct {
	log           logrus.FieldLogger
	rootDirectory string
}

func (xattrClient *FilesystemBasedXattrClient) getPathWithKey(path string, key string, addUserPrefix bool) string {
	if addUserPrefix {
		key = getKeyWithUserAttributePrefix(key)
	}
	return fmt.Sprintf("%s%s%s", path, delimiter, key)
}

func (xattrClient *FilesystemBasedXattrClient) IsSupported() (bool, error) {
	return true, nil
}

// getMetaFilenameForManifestFile Finds the base of the filename and the directory for the manifest metadata files
func (xattrClient *FilesystemBasedXattrClient) getMetaFilenamePrefixForManifestFile(path string) (fileName string, directory string) {
	relativePath := path[len(xattrClient.rootDirectory):]
	fileName = filepath.Join(xattrClient.rootDirectory, filesystemXattrMetaDataDirectoryName, relativePath)
	directory = filepath.Dir(fileName)
	return
}

func (c *FilesystemBasedXattrClient) Set(tempFileName string, fileName string, key string, value string) error {
	newPath, directory := c.getMetaFilenamePrefixForManifestFile(fileName)
	err := os.MkdirAll(directory, 0o700)
	if err != nil {
		return err
	}
	return os.WriteFile(c.getPathWithKey(newPath, key, true), []byte(value), 0o600)
}

func (c *FilesystemBasedXattrClient) Get(path, key string) (string, bool, error) {
	newPath, _ := c.getMetaFilenamePrefixForManifestFile(path)
	data, err := os.ReadFile(c.getPathWithKey(newPath, key, true))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			err = errors.Wrapf(err, "Attribute %s not found for file %s", key, path)
		}
		return "", false, err
	}
	return string(data), true, nil
}

func (c *FilesystemBasedXattrClient) list(path string, stripUserPrefix bool) ([]string, error) {
	var keys []string
	_, directory := c.getMetaFilenamePrefixForManifestFile(path)
	files, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return keys, nil
	}
	for _, file := range files {
		prefix := c.getPathWithKey(filepath.Base(path), "", stripUserPrefix)
		if strings.HasPrefix(file.Name(), prefix) {
			key := strings.TrimPrefix(file.Name(), prefix)
			if stripUserPrefix {
				key = removeUserAttributePrefixFromKey(key)
			}
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (c *FilesystemBasedXattrClient) List(path string) ([]string, error) {
	return c.list(path, true)
}

func (c *FilesystemBasedXattrClient) remove(path string, key string) error {
	newPath, _ := c.getMetaFilenamePrefixForManifestFile(path)
	err := os.Remove(c.getPathWithKey(newPath, key, false))
	if !errors.Is(err, os.ErrNotExist) {
		return errors.Wrapf(err, "Could not delete attribute %s for file %s", key, path)
	}
	//TODO: clean up parent directories.
	return nil
}

func (c *FilesystemBasedXattrClient) RemoveAll(path string) error {
	keys, err := c.list(path, false)
	if err != nil {
		return errors.Wrapf(err, "could not delete keys in path %s", path)
	}
	for _, key := range keys {
		err = c.remove(path, key)
		if err != nil {
			return errors.Wrapf(err, "could not delete key %s in path %s", key, path)
		}
	}
	return nil
}
