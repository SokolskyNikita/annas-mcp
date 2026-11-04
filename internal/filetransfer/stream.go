package filetransfer

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// ReadError distinguishes a failed source from a local write failure. An AA
// transfer with a known expected hash may resume only the former.
type ReadError struct{ Err error }

func (e *ReadError) Error() string { return e.Err.Error() }
func (e *ReadError) Unwrap() error { return e.Err }

// WriteError marks a destination failure without assigning a provider category.
type WriteError struct{ Err error }

func (e *WriteError) Error() string { return e.Err.Error() }
func (e *WriteError) Unwrap() error { return e.Err }

// Copy streams with a byte bound and cancellation between reads. onWrite sees
// the bytes committed by each write, so callers can maintain resumed progress.
func Copy(ctx context.Context, destination io.Writer, source io.Reader, maximum int64, onWrite func(int)) (int64, error) {
	var written int64
	buffer := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		n, readErr := source.Read(buffer)
		if n > 0 {
			if written+int64(n) > maximum {
				return written, fmt.Errorf("download exceeded %d bytes", maximum)
			}
			count, err := destination.Write(buffer[:n])
			written += int64(count)
			if onWrite != nil && count > 0 {
				onWrite(count)
			}
			if err != nil {
				return written, &WriteError{Err: err}
			}
			if count != n {
				return written, &WriteError{Err: io.ErrShortWrite}
			}
		}
		if errors.Is(readErr, io.EOF) {
			return written, nil
		}
		if readErr != nil {
			return written, &ReadError{Err: readErr}
		}
	}
}
