package filemiddleware

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"
)

func NewResponder(next middleware.Responder, fname string, length int64) middleware.Responder {
	return &FileMiddlewareResponder{
		next:     next,
		fileName: fname,
		length:   length,
	}
}

type FileMiddlewareResponder struct {
	next     middleware.Responder
	fileName string
	length   int64
}

func (f *FileMiddlewareResponder) WriteResponse(rw http.ResponseWriter, r runtime.Producer) {
	rw.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", f.fileName))
	if f.length != 0 {
		rw.Header().Set("Content-Length", strconv.FormatInt(f.length, 10))
	}
	f.next.WriteResponse(rw, r)
}

func (f *FileMiddlewareResponder) GetNext() middleware.Responder {
	return f.next
}
