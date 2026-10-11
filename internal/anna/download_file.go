package anna

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

var formatPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,16}$`)

var unsafeFilenameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

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
		format = formatFromResponse(resp)
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

	paths := downloadPathCandidates(folderPath, title, format, expectedMD5)
	for _, path := range paths {
		if err := os.Link(temp.Name(), path); err == nil {
			return DownloadResult{Path: path, Bytes: transfer.written}, nil
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
	buffer := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			if t.written+int64(read) > maxDownloadBytes {
				return fmt.Errorf("download exceeded %d bytes", maxDownloadBytes)
			}
			if _, err := t.hasher.Write(buffer[:read]); err != nil {
				return err
			}
			writtenNow, err := t.destination.Write(buffer[:read])
			if err != nil {
				return fmt.Errorf("failed to write file (wrote %d bytes): %w", t.written, err)
			}
			if writtenNow != read {
				return fmt.Errorf("failed to write file (wrote %d bytes): %w", t.written, io.ErrShortWrite)
			}
			t.written += int64(read)
			if t.progress != nil && t.written-t.reported >= 512*1024 {
				t.progress(t.written, max(t.size, 0))
				t.reported = t.written
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return &interruptedError{written: t.written, err: readErr}
		}
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
	if start, ok := contentRangeStart(resp.Header.Get("Content-Range")); resp.StatusCode != http.StatusPartialContent || !ok || start != t.written {
		resp.Body.Close()
		return nil, fmt.Errorf("%s did not continue the download (HTTP status %d)", responseHost(resp, rawURL), resp.StatusCode)
	}
	return resp, nil
}

// contentRangeStart reads the first byte of a Content-Range such as
// "bytes 1000-1999/2000".
func contentRangeStart(value string) (int64, bool) {
	spec, ok := strings.CutPrefix(strings.TrimSpace(value), "bytes ")
	if !ok {
		return 0, false
	}
	first, _, ok := strings.Cut(spec, "-")
	if !ok {
		return 0, false
	}
	start, err := strconv.ParseInt(first, 10, 64)
	return start, err == nil && start >= 0
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
		return "", apperr.New(apperr.InvalidArgument, "book hash must be a 32-character MD5")
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return "", apperr.New(apperr.InvalidArgument, "book hash must be a hexadecimal MD5")
	}
	return hash, nil
}

func normalizeFormat(format string) (string, error) {
	format = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(format), "."))
	if format == "" {
		return "bin", nil
	}
	if !formatPattern.MatchString(format) {
		return "", apperr.New(apperr.InvalidArgument, "file format must be a simple extension")
	}
	return format, nil
}

func downloadPathCandidates(folder, title, format, hash string) []string {
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
	names := []string{safeTitle + "." + format}
	if suffix != "" {
		names = append(names, safeTitle+"-"+suffix+"."+format)
	}
	for i := 2; i <= 20; i++ {
		names = append(names, fmt.Sprintf("%s-%s-%d.%s", safeTitle, suffix, i, format))
	}
	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(folder, name))
	}
	return paths
}

type bufferedReadCloser struct {
	*bufio.Reader
	closer io.Closer
}

func (b *bufferedReadCloser) Close() error {
	return b.closer.Close()
}

func isHTMLResponse(resp *http.Response) (bool, error) {
	if resp == nil {
		return false, errors.New("download response is nil")
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err == nil && (mediaType == "text/html" || strings.HasSuffix(mediaType, "+html")) {
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
	lower := bytes.ToLower(bytes.TrimSpace(peek))
	return bytes.HasPrefix(lower, []byte("<!doctype html")) || bytes.HasPrefix(lower, []byte("<html")), nil
}

func formatFromResponse(resp *http.Response) string {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if filename, ok := params["filename"]; ok {
				if ext := strings.TrimPrefix(filepath.Ext(filename), "."); ext != "" {
					return ext
				}
			}
		}
	}
	if ext := extensionFromMediaType(resp.Header.Get("Content-Type")); ext != "" {
		return ext
	}
	// Anna's file servers send "attachment" without a filename and a generic
	// octet-stream type, but the signed URL ends in the archived filename.
	if resp.Request != nil && resp.Request.URL != nil {
		if ext := strings.TrimPrefix(path.Ext(resp.Request.URL.Path), "."); formatPattern.MatchString(ext) {
			return ext
		}
	}
	return "bin"
}

func extensionFromMediaType(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" || mediaType == "application/octet-stream" || mediaType == "binary/octet-stream" {
		return ""
	}
	extensions, _ := mime.ExtensionsByType(mediaType)
	if len(extensions) == 0 {
		return ""
	}
	return strings.TrimPrefix(extensions[0], ".")
}

func sanitizeFilename(filename string) string {
	safe := unsafeFilenameChars.ReplaceAllString(filename, "_")
	safe = strings.ReplaceAll(safe, "..", "_")
	safe = filepath.Base(safe)
	safe = strings.TrimRight(safe, " .")
	if safe == "" {
		return ""
	}

	upper := strings.ToUpper(safe)
	stem := upper
	if dot := strings.IndexByte(stem, '.'); dot >= 0 {
		stem = stem[:dot]
	}
	stem = strings.TrimRight(stem, " .")
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		safe = "_" + safe
	}

	const maxFilenameBytes = 180
	if len(safe) > maxFilenameBytes {
		var b strings.Builder
		b.Grow(maxFilenameBytes)
		for _, r := range safe {
			size := utf8.RuneLen(r)
			if size < 0 || b.Len()+size > maxFilenameBytes {
				break
			}
			b.WriteRune(r)
		}
		safe = strings.TrimRight(b.String(), " .")
	}
	return safe
}
