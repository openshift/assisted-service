package ignition

import (
	"embed"
	"io/fs"
)

//go:embed templates
var templatesFS embed.FS

var templatesRoot fs.FS

func init() {
	var err error
	templatesRoot, err = fs.Sub(templatesFS, "templates")
	if err != nil {
		panic(err)
	}
}
