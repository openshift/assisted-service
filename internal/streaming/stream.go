package streaming

import (
	"context"
	"errors"
	"time"
)

// Stream represents a stream of items.
type Stream[I any] interface {
	// Next resturns the next item from the stream. Returns the EOS error if there are
	// no more items. Other errors may also be returned. For example, if the stream is backed
	// by a database table the database connection may fail and generate an error.
	Next(ctx context.Context) (item I, err error)
}

// Sizer is an interface that can optionally be implemented by streams that know what is their
// size. Some functions, for example the Slice function that builds an slice from a stream, can
// take advantage of this to allocate the required space in advance.
type Sizer interface {
	// Returns the number of items still available in the stream.
	Size(ctx context.Context) (size int, err error)
}

// EOS is the error returned by by the Next method of streams when there are no more items
// in the stream.
var EOS = errors.New("EOS")

// StreamFunc creates an implementation of the Stream interface using the given function.
type StreamFunc[I any] func(context.Context) (I, error)

func (f StreamFunc[I]) Next(ctx context.Context) (item I, err error) {
	return f(ctx)
}

// Pour creates a stream that contains the items in the given slice.
func Pour[I any](slice []I) Stream[I] {
	return &pourStream[I]{
		slice: slice,
	}
}

// pourStream is the implementation of the streams returned by the Pour function.
type pourStream[I any] struct {
	slice []I
}

func (s *pourStream[I]) Next(ctx context.Context) (item I, err error) {
	if len(s.slice) > 0 {
		item = s.slice[0]
		s.slice = s.slice[1:]
	} else {
		s.slice = nil
		err = EOS
	}
	return
}

func (s *pourStream[I]) Size(ctx context.Context) (size int, err error) {
	size = len(s.slice)
	return
}

// Collect collects all the items in the given stream and returns an slice containing them.
func Collect[I any](ctx context.Context, stream Stream[I]) (slice []I, err error) {
	// If we know the size of the stream we can create the slice with the reuquired capacity
	// in advance and avoid the reallocations that will otherwise be done as items are added.
	var buffer []I
	sizer, ok := stream.(Sizer)
	if ok {
		var size int
		size, err = sizer.Size(ctx)
		if err != nil {
			return
		}
		if size > 0 {
			buffer = make([]I, 0, size)
		}
	}

	// Collect the items of the stream remembering to stop if the context is canceled:
	for {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		default:
		}
		var item I
		item, err = stream.Next(ctx)
		if errors.Is(err, EOS) {
			slice = buffer
			return
		}
		if err != nil {
			return
		}
		buffer = append(buffer, item)
	}
}

// Mapper is a function that transofms one object into another.
type Mapper[F, T any] func(context.Context, F) (T, error)

// Map creates a stream that contains the result of transforming the objects of the given stream
// with a mapper. Note that the actual calls to the mapper will not happen when this function is
// called, they will happen only when the stream is eventually consumed.
func Map[F, T any](source Stream[F], mapper Mapper[F, T]) Stream[T] {
	return &mapStream[F, T]{
		source: source,
		mapper: mapper,
	}
}

// mapStream is the implementation of the streams returned by the Map function.
type mapStream[F, T any] struct {
	source Stream[F]
	mapper Mapper[F, T]
}

func (s *mapStream[F, T]) Next(ctx context.Context) (item T, err error) {
	tmp, err := s.source.Next(ctx)
	if err != nil {
		return
	}
	item, err = s.mapper(ctx, tmp)
	return
}

// Selector is a function that filters element of a stream.
type Selector[I any] func(context.Context, I) (bool, error)

// Select creates a new stream that only contains the items of the source stream that return true
// for the given selector. Note that the actual calls to the select will not happen when this
// function is called, they will happen only when the stream is eventually consumed.
func Select[I any](source Stream[I], selector Selector[I]) Stream[I] {
	return &selectStream[I]{
		source:   source,
		selector: selector,
	}
}

// selectStream is the implementation of streams returned by the Select function.
type selectStream[I any] struct {
	source   Stream[I]
	selector Selector[I]
}

func (s *selectStream[I]) Next(ctx context.Context) (item I, err error) {
	for {
		var tmp I
		tmp, err = s.source.Next(ctx)
		if err != nil {
			return
		}
		var ok bool
		ok, err = s.selector(ctx, tmp)
		if err != nil {
			return
		}
		if ok {
			item = tmp
			return
		}
	}
}

// Delay creates a new stream that returns the same items than the given source, but with an
// additional delay for each item. This is intended for tests and there is usually no reason
// to use in production code.
func Delay[I any](source Stream[I], delay time.Duration) Stream[I] {
	return &delayStream[I]{
		source: source,
		delay:  delay,
	}
}

// delayStream is the implementation of streams returned by the Delay function.
type delayStream[I any] struct {
	source Stream[I]
	delay  time.Duration
}

func (s *delayStream[I]) Next(ctx context.Context) (item I, err error) {
	item, err = s.source.Next(ctx)
	if err != nil {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(s.delay):
	}
	return
}
