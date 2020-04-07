package filemiddleware

import (
	"fmt"
	"net/http"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"
)

func NewResponder(next middleware.Responder, fname string) middleware.Responder {
	return &fileMiddlewareResponder{
		next:     next,
		fileName: fname,
	}
}

type fileMiddlewareResponder struct {
	next     middleware.Responder
	fileName string
}

func (f *fileMiddlewareResponder) WriteResponse(rw http.ResponseWriter, r runtime.Producer) {
	rw.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", f.fileName))
	f.next.WriteResponse(rw, r)
}
