package templating

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestTemplating(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Templating")
}

// Tmp creates a temporary directory containing the given files, and then creates a fs.FS object that can be used to
// access it.
//
// The files are specified as pairs of full path names and content. For example, to create a file named
// `mydir/myfile.yaml` containig some YAML text and a file `yourdir/yourfile.json` containing some JSON text:
//
//	dir, fsys = Tmp(
//		"mydir/myfile.yaml",
//		`
//			name: Joe
//			age: 52
//		`,
//		"yourdir/yourfile.json",
//		`{
//			"name": "Mary",
//			"age": 59
//		}`
//	)
//
// Directories are created automatically when they contain at least one file or subdirectory.
//
// The caller is responsible for removing the directory once it is no longer needed.
func Tmp(args ...any) (dir string, fsys fs.FS) {
	Expect(len(args) % 2).To(BeZero())
	dir, err := os.MkdirTemp("", "*.test")
	Expect(err).ToNot(HaveOccurred())
	for i := 0; i < len(args)/2; i++ {
		name := args[2*i].(string)
		text := args[2*i+1]
		file := filepath.Join(dir, name)
		sub := filepath.Dir(file)
		_, err = os.Stat(sub)
		if errors.Is(err, os.ErrNotExist) {
			err = os.MkdirAll(sub, 0700)
			Expect(err).ToNot(HaveOccurred())
		} else {
			Expect(err).ToNot(HaveOccurred())
		}
		switch typed := text.(type) {
		case string:
			err = os.WriteFile(file, []byte(typed), 0600)
		case []byte:
			err = os.WriteFile(file, typed, 0600)
		}
		Expect(err).ToNot(HaveOccurred())
	}
	fsys = os.DirFS(dir)
	return
}
