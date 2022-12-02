package streaming

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// QueryBuilder contains the data and logic needed to build a query stream. Don't create instances
// of this directly, use the NewQuery function instead.
type QueryBuilder[R any] struct {
	db      *sql.DB
	text    string
	fetch   int
	scanner func(*sql.Rows) (R, error)
}

// Query is a stream that returns the results of a SQL query. Don't create instances of this
// directly, use the NewQuery function instead.
type Query[R any] struct {
	db      *sql.DB
	tx      *sql.Tx
	text    string
	fetch   int
	scanner func(*sql.Rows) (R, error)
	cursor  string
	buffer  []R
	eos     bool
}

// NewQuery creats a builder that can then be used to create a query stream.
func NewQuery[R any]() *QueryBuilder[R] {
	return &QueryBuilder[R]{
		fetch: 1,
	}
}

// DB sets the database handle that will be used to execute the query. This is mandatory.
func (b *QueryBuilder[R]) DB(value *sql.DB) *QueryBuilder[R] {
	b.db = value
	return b
}

// Text sets the SQL text of the query. This is mandatory.
func (b *QueryBuilder[R]) Text(value string) *QueryBuilder[R] {
	b.text = value
	return b
}

// Fetch sets the number of rows that will be fetched from the database each time that new rows
// are needed. The default is to retrieve one row each time. It is usually desirable to fetch
// multiple rows each time, but it requires more memory.
func (b *QueryBuilder[R]) Fetch(value int) *QueryBuilder[R] {
	b.fetch = value
	return b
}

// Scanner is the function that will be used to convert a rows into items. This is mandatory.
func (b *QueryBuilder[R]) Scanner(value func(*sql.Rows) (R, error)) *QueryBuilder[R] {
	b.scanner = value
	return b
}

// Build uses the information stored in the builder to configure and create a query stream.
func (b *QueryBuilder[R]) Build() (result *Query[R], err error) {
	// Check parameters:
	if b.db == nil {
		err = errors.New("database handle is mandatory")
		return
	}
	if b.text == "" {
		err = errors.New("text is mandatory")
		return
	}
	if b.scanner == nil {
		err = errors.New("scanner is mandatory")
		return
	}
	if b.fetch < 1 {
		err = fmt.Errorf(
			"fetch count should be greater or equal than one, but it is %d",
			b.fetch,
		)
		return
	}

	// Create and populate the object:
	result = &Query[R]{
		db:      b.db,
		text:    b.text,
		fetch:   b.fetch,
		scanner: b.scanner,
	}
	return
}

func (s *Query[R]) Next(ctx context.Context) (row R, err error) {
	// Always remember to close the transaction in case of error, including the end of the
	// stream. These transactions are read only, so it is OK to always roll them back.
	defer func() {
		if err != nil && s.tx != nil {
			s.tx.Rollback()
			s.tx = nil
		}
	}()

	// Exit if we have already reached the end of the stream:
	if s.eos {
		err = EOS
		return
	}

	// Stop if the context is canceled:
	select {
	case <-ctx.Done():
		err = ctx.Err()
		return
	default:
	}

	// Start the transaction if needed:
	if s.tx == nil {
		s.tx, err = s.db.BeginTx(ctx, &sql.TxOptions{
			ReadOnly: true,
		})
		if err != nil {
			return
		}
	}

	// Open the cursor if needed:
	if s.cursor == "" {
		s.cursor = uuid.New().String()
		_, err = s.tx.ExecContext(
			ctx,
			`select open_cursor($1, $2)`,
			s.cursor,
			s.text,
		)
		if err != nil {
			return
		}
	}

	// If the current buffer hasn't been exahusted then return the first item from it:
	if len(s.buffer) > 0 {
		row = s.buffer[0]
		s.buffer = s.buffer[1:]
		return
	}

	// Fill the buffer:
	rows, err := s.tx.QueryContext(
		ctx,
		fmt.Sprintf(`fetch %d from "%s"`, s.fetch, s.cursor),
	)
	if err != nil {
		return
	}
	s.buffer = nil
	for rows.Next() {
		var tmp R
		tmp, err = s.scanner(rows)
		if err != nil {
			return
		}
		s.buffer = append(s.buffer, tmp)
	}

	// If at this point the buffer is empty then there are no more rows:
	if len(s.buffer) == 0 {
		s.eos = true
		err = EOS
		return
	}

	// Return the first row from the buffer:
	row = s.buffer[0]
	s.buffer = s.buffer[1:]
	return
}
