package filemiddleware

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"
)

func NewResponder(next middleware.Responder, fname string, length int64) middleware.Responder {
	return &fileMiddlewareResponder{
		next:     next,
		fileName: fname,
		length:   length,
	}
}

type fileMiddlewareResponder struct {
	next     middleware.Responder
	fileName string
	length   int64
}

func (f *fileMiddlewareResponder) WriteResponse(rw http.ResponseWriter, r runtime.Producer) {
	rw.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", f.fileName))
	if f.length != 0 {
		rw.Header().Set("Content-Length", strconv.FormatInt(f.length, 10))
	}
	f.next.WriteResponse(rw, r)
}
