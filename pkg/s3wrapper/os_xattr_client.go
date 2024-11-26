package s3wrapper

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/google/renameio"
	"github.com/pkg/errors"
	"github.com/pkg/xattr"
	"github.com/sirupsen/logrus"
)

func NewOSxAttrClient(
	log logrus.FieldLogger,
	rootDirectory string,
) *OSxattrClient {
	return &OSxattrClient{
		log:           log,
		rootDirectory: rootDirectory,
	}
}

// OSxattrClient represents an xattr client that uses the OS native xattr functionality (if available.)
type OSxattrClient struct {
	log           logrus.FieldLogger
	rootDirectory string
}

func (c *OSxattrClient) hasUserAttributePrefix(key string) bool {
	return strings.HasPrefix(key, xattrUserAttributePrefix)
}

func (c *OSxattrClient) IsSupported() (bool, error) {
	err := os.MkdirAll(c.rootDirectory, 0o700)
	if err != nil {
		return false, errors.Wrap(err, "Unable to create initial directories")
	}
	t, err := renameio.TempFile("", filepath.Join(c.rootDirectory, "testXattr"))
	if err != nil {
		return false, errors.Wrap(err, "Unable to create temp file to detect xattr capabilities")
	}
	defer func() {
		if err = t.Cleanup(); err != nil {
			c.log.Warn("Unable to clean up temp file %s", t.Name())
		}
	}()
	err = c.Set(t.Name(), "", "user.test-xattr-attribute-set", "foobar")
	if err != nil {
		c.log.Warnf("The file system at '%s' doesn't support extended attributes. This happens when using a RHEL NFS server older than 8.4, a NetApp ONTAP older than 9.12.1, or some other file system that doesn't support extended attributes.", c.rootDirectory)
		return false, nil
	}
	c.log.Info("OSxattrClient is supported and is enabled")
	return true, nil
}

func (c *OSxattrClient) Set(tempFileName string, fileName string, key string, value string) error {
	key = getKeyWithUserAttributePrefix(key)
	return xattr.Set(tempFileName, key, []byte(value))
}

func (c *OSxattrClient) Get(path, key string) (string, bool, error) {
	key = getKeyWithUserAttributePrefix(key)
	value, err := xattr.Get(path, key)
	if errors.Is(err, syscall.ENODATA) {
		return "", false, nil
	}
	if err != nil {
		return "", false, errors.Wrap(err, "Unable to obtain extended file attributes while retrieving file metadata")
	}
	return string(value), true, nil
}

func (c *OSxattrClient) List(path string) ([]string, error) {
	keys := []string{}
	result, err := xattr.List(path)
	if err != nil {
		return nil, err
	}
	for i := range result {
		key := result[i]
		if c.hasUserAttributePrefix(key) {
			key = removeUserAttributePrefixFromKey(result[i])
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (c *OSxattrClient) RemoveAll(path string) error {
	/*
	* As bulk metadata removal is only used on file deletion, we do not need to perform this
	* in fact it is best that we do not as often the file will already have been deleted
	* The OS xattr implementation stores metadata in the file itself - no file -- no metadata
	 */
	return nil
}
