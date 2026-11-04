package anna

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"iter"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/SokolskyNikita/annas-mcp/anna/internal/parse"
	"github.com/SokolskyNikita/annas-mcp/internal/filetransfer"
)

var formatPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,16}$`)

const maxDownloadBytes int64 = 8 << 30

// getFunc requests rawURL with extra headers, such as Range.
type getFunc func(ctx context.Context, client *http.Client, rawURL string, header http.Header) (*http.Response, error)

// maxResumes is how often an interrupted download continues where it
// stopped before the attempt counts as failed.
const maxResumes = 2

func downloadFileWithGetter(ctx context.Context, client *http.Client, rawURL, folderPath, title, format, expectedMD5 string, progress ProgressFunc, get getFunc) (DownloadResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	resp, err := get(ctx, client, rawURL, nil)
	if err != nil {
		return DownloadResult{}, requestFailure(err, rawURL)
	}
	// A resumed download replaces resp; close whichever is current.
	defer func() { resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return DownloadResult{}, httpResponseError(resp, rawURL)
	}
	if resp.ContentLength > maxDownloadBytes {
		return DownloadResult{}, fmt.Errorf("download is larger than %d bytes", maxDownloadBytes)
	}
	isHTML, err := isHTMLResponse(resp)
	if err != nil {
		return DownloadResult{}, err
	}
	if isHTML {
		return DownloadResult{}, fmt.Errorf("download from %s returned HTML instead of a file (possible access challenge)", responseHost(resp, rawURL))
	}

	if format == "" {
		format = parse.DownloadFormat(resp.Header, requestURL(resp))
	}
	format, err = normalizeFormat(format)
	if err != nil {
		return DownloadResult{}, err
	}

	if err := os.MkdirAll(folderPath, 0o755); err != nil {
		return DownloadResult{}, fmt.Errorf("failed to create download directory: %w", err)
	}

	temp, err := os.CreateTemp(folderPath, ".annas-mcp-download-*.part")
	if err != nil {
		return DownloadResult{}, err
	}
	defer func() {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
	}()

	transfer, err := newTransfer(temp, resp.ContentLength, expectedMD5, progress)
	if err != nil {
		return DownloadResult{}, err
	}
	for resumes := 0; ; resumes++ {
		err = transfer.copy(ctx, resp.Body)
		var interrupted *interruptedError
		if err == nil || resumes == maxResumes || !errors.As(err, &interrupted) || ctx.Err() != nil {
			break
		}
		resp.Body.Close()
		next, resumeErr := transfer.resume(ctx, client, rawURL, get)
		if resumeErr != nil {
			err = fmt.Errorf("%w; resuming failed: %v", err, resumeErr)
			break
		}
		resp = next
	}
	if err == nil {
		err = transfer.finish()
	}
	if err != nil {
		return DownloadResult{}, err
	}
	if err := temp.Sync(); err != nil {
		return DownloadResult{}, fmt.Errorf("failed to sync file to disk: %w", err)
	}
	if err := temp.Chmod(0o644); err != nil {
		return DownloadResult{}, fmt.Errorf("failed to set downloaded file mode: %w", err)
	}
	if err := temp.Close(); err != nil {
		return DownloadResult{}, fmt.Errorf("failed to close downloaded file: %w", err)
	}

	for path := range downloadPathCandidates(folderPath, title, format, expectedMD5) {
		if err := ctx.Err(); err != nil {
			return DownloadResult{}, err
		}
		if err := os.Link(temp.Name(), path); err == nil {
			return DownloadResult{Path: path, Bytes: transfer.written, MD5: hex.EncodeToString(transfer.hasher.Sum(nil))}, nil
		} else if !errors.Is(err, os.ErrExist) {
			return DownloadResult{}, fmt.Errorf("failed to finalize downloaded file: %w", err)
		}
	}
	return DownloadResult{}, errors.New("could not find a free filename")
}

// interruptedError is a download whose body stopped before its end.
type interruptedError struct {
	written int64
	err     error
}

func (e *interruptedError) Error() string {
	return fmt.Sprintf("download stopped after %d bytes: %v", e.written, e.err)
}

func (e *interruptedError) Unwrap() error { return e.err }

// transfer writes a download, hashing it as it goes, so an interrupted
// download can continue where it stopped.
type transfer struct {
	destination io.Writer
	hasher      hash.Hash
	written     int64
	reported    int64
	// size is the expected size, or -1 when the server did not say.
	size     int64
	expected string
	progress ProgressFunc
}

func newTransfer(destination io.Writer, size int64, expectedMD5 string, progress ProgressFunc) (*transfer, error) {
	if expectedMD5 != "" {
		normalized, err := normalizeHash(expectedMD5)
		if err != nil {
			return nil, err
		}
		expectedMD5 = normalized
	}
	return &transfer{destination: destination, hasher: md5.New(), size: size, expected: expectedMD5, progress: progress}, nil
}

// copy appends source. A read that fails, or a body that ends before size,
// returns an *interruptedError.
func (t *transfer) copy(ctx context.Context, source io.Reader) error {
	_, err := filetransfer.Copy(ctx, io.MultiWriter(t.hasher, t.destination), source, maxDownloadBytes-t.written, func(count int) {
		t.written += int64(count)
		if t.progress != nil && t.written-t.reported >= 512*1024 {
			t.progress(t.written, max(t.size, 0))
			t.reported = t.written
		}
	})
	if err != nil {
		var writeError *filetransfer.WriteError
		if errors.As(err, &writeError) {
			return fmt.Errorf("failed to write file (wrote %d bytes): %w", t.written, writeError.Err)
		}
		var sourceError *filetransfer.ReadError
		if errors.As(err, &sourceError) {
			return &interruptedError{written: t.written, err: sourceError.Err}
		}
		return err
	}

	if t.size >= 0 && t.written < t.size {
		return &interruptedError{written: t.written, err: io.ErrUnexpectedEOF}
	}
	return nil
}

// resume asks for the rest of the file. Only a 206 answer that starts
// exactly where the download stopped continues it; any other answer fails
// the attempt, and the caller tries the next server.
func (t *transfer) resume(ctx context.Context, client *http.Client, rawURL string, get getFunc) (*http.Response, error) {
	resp, err := get(ctx, client, rawURL, http.Header{"Range": {fmt.Sprintf("bytes=%d-", t.written)}})
	if err != nil {
		return nil, requestFailure(err, rawURL)
	}
	if start, ok := parse.ContentRangeStart(resp.Header.Get("Content-Range")); resp.StatusCode != http.StatusPartialContent || !ok || start != t.written {
		resp.Body.Close()
		return nil, fmt.Errorf("%s did not continue the download (HTTP status %d)", responseHost(resp, rawURL), resp.StatusCode)
	}
	return resp, nil
}

// finish checks the complete download.
func (t *transfer) finish() error {
	if t.progress != nil && t.reported != t.written {
		t.progress(t.written, max(t.size, 0))
		t.reported = t.written
	}
	if t.written == 0 {
		return errors.New("download returned an empty file")
	}
	if t.size >= 0 && t.written != t.size {
		return fmt.Errorf("download size mismatch: expected %d bytes, wrote %d", t.size, t.written)
	}
	if t.expected != "" && !strings.EqualFold(hex.EncodeToString(t.hasher.Sum(nil)), t.expected) {
		return fmt.Errorf("downloaded file checksum does not match %s", t.expected)
	}
	return nil
}

func normalizeHash(hash string) (string, error) {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if len(hash) != md5.Size*2 {
		return "", NewError(InvalidArgument, "book hash must be a 32-character MD5")
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return "", NewError(InvalidArgument, "book hash must be a hexadecimal MD5")
	}
	return hash, nil
}

func normalizeFormat(format string) (string, error) {
	format = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(format), "."))
	if format == "" {
		return "bin", nil
	}
	if !formatPattern.MatchString(format) {
		return "", NewError(InvalidArgument, "file format must be a simple extension")
	}
	return format, nil
}

func downloadPathCandidates(folder, title, format, hash string) iter.Seq[string] {
	safeTitle := sanitizeFilename(title)
	if safeTitle == "" {
		safeTitle = "untitled"
	}
	suffix := strings.TrimSpace(hash)
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	var b strings.Builder
	for _, r := range suffix {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	suffix = b.String()
	return func(yield func(string) bool) {
		if !yield(filepath.Join(folder, safeTitle+"."+format)) {
			return
		}
		if suffix != "" && !yield(filepath.Join(folder, safeTitle+"-"+suffix+"."+format)) {
			return
		}
		for i := 2; ; i++ {
			if !yield(filepath.Join(folder, fmt.Sprintf("%s-%s-%d.%s", safeTitle, suffix, i, format))) {
				return
			}
		}
	}
}

type bufferedReadCloser struct {
	*bufio.Reader
	closer io.Closer
}

func (b *bufferedReadCloser) Close() error {
	return b.closer.Close()
}

func sanitizeFilename(filename string) string {
	return filetransfer.SanitizeFilename(filename)
}

func isHTMLResponse(resp *http.Response) (bool, error) {
	if resp == nil {
		return false, errors.New("download response is nil")
	}
	if parse.LooksLikeHTML(resp.Header.Get("Content-Type"), nil) {
		return true, nil
	}
	if resp.Body == nil {
		return false, errors.New("download response has no body")
	}
	reader := bufio.NewReader(resp.Body)
	resp.Body = &bufferedReadCloser{Reader: reader, closer: resp.Body}
	peek, peekErr := reader.Peek(512)
	if peekErr != nil && !errors.Is(peekErr, io.EOF) {
		return false, fmt.Errorf("failed to inspect download response: %w", peekErr)
	}
	return parse.LooksLikeHTML("", peek), nil
}

// requestURL is the URL a response answered, after redirects.
func requestURL(resp *http.Response) *url.URL {
	if resp.Request == nil {
		return nil
	}
	return resp.Request.URL
}
