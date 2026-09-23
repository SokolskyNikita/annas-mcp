package anna

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

var formatPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,16}$`)

var unsafeFilenameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

const maxDownloadBytes int64 = 8 << 30

type getFunc func(context.Context, *http.Client, string) (*http.Response, error)

func downloadFileWithGetter(ctx context.Context, client *http.Client, rawURL, folderPath, title, format, expectedMD5 string, progress ProgressFunc, get getFunc) (DownloadResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	resp, err := get(ctx, client, rawURL)
	if err != nil {
		return DownloadResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return DownloadResult{}, fmt.Errorf("download failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if resp.ContentLength > maxDownloadBytes {
		return DownloadResult{}, fmt.Errorf("download is larger than %d bytes", maxDownloadBytes)
	}
	isHTML, err := isHTMLResponse(resp)
	if err != nil {
		return DownloadResult{}, err
	}
	if isHTML {
		return DownloadResult{}, errors.New("download returned an HTML page instead of a file")
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

	written, err := copyWithHash(ctx, temp, resp.Body, resp.ContentLength, expectedMD5, progress)
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
			return DownloadResult{Path: path, Bytes: written}, nil
		} else if !errors.Is(err, os.ErrExist) {
			return DownloadResult{}, fmt.Errorf("failed to finalize downloaded file: %w", err)
		}
	}
	return DownloadResult{}, errors.New("could not find a free filename")
}

func copyWithHash(ctx context.Context, destination io.Writer, source io.Reader, contentLength int64, expectedMD5 string, progress ProgressFunc) (int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if expectedMD5 != "" {
		normalized, err := normalizeHash(expectedMD5)
		if err != nil {
			return 0, err
		}
		expectedMD5 = normalized
	}
	hasher := md5.New()
	buffer := make([]byte, 32*1024)
	var written int64
	var reported int64
	total := contentLength
	if total < 0 {
		total = 0
	}

	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			if written+int64(read) > maxDownloadBytes {
				return written, fmt.Errorf("download exceeded %d bytes", maxDownloadBytes)
			}
			if _, err := hasher.Write(buffer[:read]); err != nil {
				return written, err
			}
			writtenNow, err := destination.Write(buffer[:read])
			if err != nil {
				return written, fmt.Errorf("failed to write file (wrote %d bytes): %w", written, err)
			}
			if writtenNow != read {
				return written, fmt.Errorf("failed to write file (wrote %d bytes): %w", written, io.ErrShortWrite)
			}
			written += int64(read)
			if progress != nil && (written-reported >= 512*1024 || readErr == io.EOF) {
				progress(written, total)
				reported = written
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return written, fmt.Errorf("failed to write file (wrote %d bytes): %w", written, readErr)
		}
	}

	if progress != nil && reported != written {
		progress(written, total)
	}
	if written == 0 {
		return 0, errors.New("download returned an empty file")
	}
	if contentLength >= 0 && written != contentLength {
		return written, fmt.Errorf("download size mismatch: expected %d bytes, wrote %d", contentLength, written)
	}
	if expectedMD5 != "" && !strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), expectedMD5) {
		return written, fmt.Errorf("downloaded file checksum does not match %s", expectedMD5)
	}
	return written, nil
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
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		return "bin"
	}
	extensions, _ := mime.ExtensionsByType(mediaType)
	if len(extensions) == 0 {
		return "bin"
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
