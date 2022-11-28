package json

import (
	"encoding/json"
	"io"

	"github.com/go-openapi/runtime"
)

// Consumer is a consumer function used in the swagger API to parse JSON documents while
// rejecting requests that contain unknown fields.
func Consumer() runtime.Consumer {
	return runtime.ConsumerFunc(func(reader io.Reader, data interface{}) error {
		dec := json.NewDecoder(reader)
		dec.UseNumber()
		dec.DisallowUnknownFields()
		return dec.Decode(data)
	})
}
