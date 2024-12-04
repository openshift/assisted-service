package s3wrapper

import (
	"slices"

	"github.com/sirupsen/logrus"
)

func NewCompositeXattrClient(
	log logrus.FieldLogger,
	oSxattrClient XattrClient,
	filesystemBasedXattrClient XattrClient,
) (*CompositeXattrClient, error) {
	oSxattrClientSupported, err := oSxattrClient.IsSupported()
	if err != nil {
		return nil, err
	}
	if !oSxattrClientSupported {
		log.Warn("The native xattr client doesn't support extended attributes. A fallback has been enabled.")
	}
	filesystemBasedXattrClientSupported, err := filesystemBasedXattrClient.IsSupported()
	if err != nil {
		return nil, err
	}
	return &CompositeXattrClient{
		oSxattrClient:                       oSxattrClient,
		filesystemBasedXattrClient:          filesystemBasedXattrClient,
		oSxattrClientSupported:              oSxattrClientSupported,
		filesystemBasedXattrClientSupported: filesystemBasedXattrClientSupported,
	}, nil
}

type CompositeXattrClient struct {
	oSxattrClient                       XattrClient
	filesystemBasedXattrClient          XattrClient
	oSxattrClientSupported              bool
	filesystemBasedXattrClientSupported bool
}

func (c *CompositeXattrClient) IsSupported() (bool, error) {
	return true, nil
}

func (c *CompositeXattrClient) Set(tempFileName string, fileName string, key string, value string) error {
	// If the native xattr writes are supported then use those
	// otherwise fall back to filesystem based writes.
	if c.oSxattrClientSupported {
		return c.oSxattrClient.Set(tempFileName, fileName, key, value)
	}
	return c.filesystemBasedXattrClient.Set(tempFileName, fileName, key, value)
}

func (c *CompositeXattrClient) Get(path, key string) (string, bool, error) {
	// Search for the record first in the oSxattrClient
	// if not found then look in the filesystemBasedXattrClient.
	var err error
	var ok bool
	var result string
	if c.oSxattrClientSupported {
		result, ok, err = c.oSxattrClient.Get(path, key)
		if err != nil {
			return "", ok, err
		}
		if ok {
			return result, ok, nil
		}
	}
	return c.filesystemBasedXattrClient.Get(path, key)
}

func (c *CompositeXattrClient) List(path string) ([]string, error) {
	// Produce a list that is the union of keys from both clients
	// Just in case the user has performed an upgrade from the filesystem based method.
	// respect that the oSxattrClient takes priority when available.
	var primaryList []string
	var secondaryList []string
	var err error
	if c.oSxattrClientSupported {
		primaryList, err = c.oSxattrClient.List(path)
		if err != nil {
			return nil, err
		}
	}
	secondaryList, err = c.filesystemBasedXattrClient.List(path)
	if err != nil {
		return nil, err
	}
	for _, secondaryListItem := range secondaryList {
		if !slices.Contains(primaryList, secondaryListItem) {
			primaryList = append(primaryList, secondaryListItem)
		}
	}
	return primaryList, nil
}

func (c *CompositeXattrClient) RemoveAll(path string) error {
	// Metadata only needs to be removed in this way for the fallback solution.
	return c.filesystemBasedXattrClient.RemoveAll(path)
}
