package streaming

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-openapi/runtime"
	jsoniter "github.com/json-iterator/go"
)

// ResponderBuilder contains the data and logic needed to build a new streaming respnder. Don't
// create instances of this directly, use the NewResponder function instead.
type ResponderBuilder[I any] struct {
	source Stream[I]
	flush  int
	ctx    context.Context
}

// Responder is an responder that takes items from a stream and writes then to the response as
// items of a JSON array. Don't create instances of this directly, use the NewResponder function
// instead.
type Responder[I any] struct {
	source Stream[I]
	flush  int
	ctx    context.Context
}

// NewResponder creates a builder that can then be used to configure and create a streaming
// responder.
func NewResponder[I any]() *ResponderBuilder[I] {
	return &ResponderBuilder[I]{}
}

// Source sets the stream that the responder will consume. This is mandatory.
func (b *ResponderBuilder[I]) Source(value Stream[I]) *ResponderBuilder[I] {
	b.source = value
	return b
}

// Flush sets how many items are written before explicitly flushing the writer. Small values reduce
// performance, as there will be more writes and buffers will be used less efficiently, but it can
// be convenient when the client needs or wants to process results one by one as soon as they
// arrive. The default value is zero which means that the writer will never be explicitly flushed.
// It may be flushed anyhow when the underlying buffers are filled.
//
// Note that flushing requires the response writer to support the http.Flusher interface. If it
// doesn't implement it then this flag will be ignored.
func (b *ResponderBuilder[I]) Flush(value int) *ResponderBuilder[I] {
	b.flush = value
	return b
}

// Context sets the context that the responder will use. The default is to use a new
// background context.
func (b *ResponderBuilder[I]) Context(value context.Context) *ResponderBuilder[I] {
	b.ctx = value
	return b
}

// Build uses the data stored in the builder to create a new streaming respnder.
func (b *ResponderBuilder[I]) Build() (result *Responder[I], err error) {
	// Check parameters:
	if b.source == nil {
		err = errors.New("stream is mandatory")
		return
	}
	if b.flush < 0 {
		err = fmt.Errorf(
			"flush must be greater or equal than zero, but it is %d",
			b.flush,
		)
	}

	// Set default values:
	ctx := b.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	// Create and populate the object:
	result = &Responder[I]{
		source: b.source,
		flush:  b.flush,
		ctx:    ctx,
	}
	return
}

// WriteResponse is the implementation of the responder interface. Note that this implementation
// ignores the producer; it always writes JSON.
func (r *Responder[I]) WriteResponse(w http.ResponseWriter, _ runtime.Producer) {
	// Get the context:
	ctx := r.ctx

	// Get the flusher:
	flusher, _ := w.(http.Flusher)

	// Create the JSON stream:
	stream := jsoniter.NewStream(jsoniter.ConfigDefault, w, 4096)

	// Get items from the source stream and write them to the JSON stream:
	r.sendArrayStart(stream)
	first := true
	count := r.flush
	for {
		item, err := r.source.Next(ctx)
		if errors.Is(err, EOS) {
			r.sendArrayEnd(stream)
			return
		}
		if !first {
			r.sendMore(stream)
		}
		r.sendItem(stream, item)
		if r.flush > 0 {
			count--
			if count == 0 {
				err = stream.Flush()
				if err != nil {
					panic(err)
				}
				if flusher != nil {
					flusher.Flush()
				}
				count = r.flush
			}
		}
		first = false
	}
}

func (r *Responder[I]) sendArrayStart(stream *jsoniter.Stream) {
	stream.WriteArrayStart()
	if stream.Error != nil {
		panic(stream.Error)
	}
	r.sendLineBreak(stream)
}

func (r *Responder[I]) sendArrayEnd(stream *jsoniter.Stream) {
	r.sendLineBreak(stream)
	stream.WriteArrayEnd()
	if stream.Error != nil {
		panic(stream.Error)
	}
	r.sendLineBreak(stream)
}

func (r *Responder[I]) sendMore(stream *jsoniter.Stream) {
	stream.WriteMore()
	if stream.Error != nil {
		panic(stream.Error)
	}
	r.sendLineBreak(stream)
}

func (h *Responder[I]) sendItem(stream *jsoniter.Stream, item I) {
	stream.WriteVal(item)
	if stream.Error != nil {
		panic(stream.Error)
	}
}

func (h *Responder[I]) sendLineBreak(stream *jsoniter.Stream) {
	stream.Write(lineBreak)
	if stream.Error != nil {
		panic(stream.Error)
	}
}

var lineBreak = []byte("\n")
