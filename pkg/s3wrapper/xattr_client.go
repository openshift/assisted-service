package s3wrapper

import (
	"fmt"
	"strings"
)

//go:generate mockgen --build_flags=--mod=mod -package=s3wrapper -destination=mock_xattr_client.go . XattrClient
type XattrClient interface {
	// IsSupported will determine if the client is supported in the current configuration
	IsSupported() (bool, error)

	// Set applies an extended attribute to a given path
	// tempFileName - If using a temp file, pass t.name() here
	// fileName - The filename that will be given to the file after any temp processing should be provided here.
	// parameter: key - The key of the extended attribute.
	// parameter: value - The value of the extended attribute.
	// returns: an error if this fails, otherwise nil.
	Set(tempFileName string, fileName string, key string, value string) error

	// Get returns a value for an extended attribute key on a given given path.
	// parameter: path - The path for which the key is to be fetched.
	// parameter: key - The key of the extended attribute.
	// returns the attribute value as a string,
	// if the attribute is valid ok will be true otherwise false
	// returns an error if there was one
	Get(path, key string) (string, bool, error)

	// List obtains a list of extended attribute keys for a given path
	// parameter: path - The path for which extended attributes are to be fetched.
	// parameter: stripUserPrefix - Should the user prefix be removed from keys or left in place?
	// returns: a list of extended attribute keys for the path or an error if this fails.
	List(path string) ([]string, error)

	// Removes all extended attributes for the file at the given path.
	// parameter: path - The path for which the extended attributes are to be deleted.
	// returns an error if there was an error
	// If the path does not contain any metdadata then this function will return nil.
	RemoveAll(path string) error
}

const (
	xattrUserAttributePrefix = "user."
)

func getKeyWithUserAttributePrefix(key string) string {
	return fmt.Sprintf("%s%s", xattrUserAttributePrefix, key)
}

func removeUserAttributePrefixFromKey(key string) string {
	return strings.TrimPrefix(key, xattrUserAttributePrefix)
}
