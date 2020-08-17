package s3wrapper

import "fmt"

type NotFound string

func (f NotFound) Error() string {
	return fmt.Sprintf("object %s was not found", string(f))
}
