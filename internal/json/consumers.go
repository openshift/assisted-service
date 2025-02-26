package json

import (
	json "github.com/bytedance/sonic"
	"io"

	"github.com/go-openapi/runtime"
)

// UnknownFieldsRejectingConsumer is a consumer function used in the swagger API to parse JSON
// documents while rejecting requests that contain unknown fields.
func UnknownFieldsRejectingConsumer() runtime.Consumer {
	return runtime.ConsumerFunc(func(reader io.Reader, data interface{}) error {
		dec := json.ConfigStd.NewDecoder(reader)
		dec.UseNumber()
		dec.DisallowUnknownFields()
		return dec.Decode(data)
	})
}
