package filemiddleware

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"
)

func NewResponder(next middleware.Responder, fname string, length int64, modifiedAt *time.Time) middleware.Responder {
	return &FileMiddlewareResponder{
		next:       next,
		fileName:   fname,
		length:     length,
		modifiedAt: modifiedAt,
	}
}

type FileMiddlewareResponder struct {
	next       middleware.Responder
	fileName   string
	length     int64
	modifiedAt *time.Time
}

func (f *FileMiddlewareResponder) WriteResponse(rw http.ResponseWriter, r runtime.Producer) {
	rw.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", f.fileName))
	if f.length != 0 {
		rw.Header().Set("Content-Length", strconv.FormatInt(f.length, 10))
	}
	if f.modifiedAt != nil {
		rw.Header().Set("Last-Modified", f.modifiedAt.UTC().Format(http.TimeFormat))
	}
	f.next.WriteResponse(rw, r)
}

func (f *FileMiddlewareResponder) GetNext() middleware.Responder {
	return f.next
}
