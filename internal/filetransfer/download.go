// Package filetransfer streams research artifacts into verified, reusable local files.
package filetransfer

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxPDFBytes int64 = 8 << 30
const maxXMLBytes int64 = 128 << 20

// Config contains explicit download settings. A custom client is intended for
// callers that supply their own trusted transport, including offline tests.
type Config struct {
	Directory  string
	CacheDir   string
	APIKey     string
	HTTPClient *http.Client
}

type Client struct {
	config Config
	http   *http.Client
}

type Request struct {
	Key      string
	URL      string
	Title    string
	Artifact string
	Hosted   bool
	Progress func(done, total int64)
}

type Result struct {
	Path     string `json:"path"`
	Bytes    int64  `json:"bytes"`
	SHA256   string `json:"sha256"`
	MIMEType string `json:"mime_type"`
	Reused   bool   `json:"reused"`
}

// Error exposes a stable category without including credential-bearing URLs.
type Error struct {
	Code   string
	Reason string
	Err    error
}

func (e *Error) Error() string     { return e.Reason }
func (e *Error) Unwrap() error     { return e.Err }
func (e *Error) ErrorCode() string { return e.Code }

func New(config Config) (*Client, error) {
	if config.Directory != "" {
		config.Directory = filepath.Clean(config.Directory)
	}
	if config.CacheDir == "" && config.Directory != "" {
		config.CacheDir = filepath.Join(config.Directory, ".annas-mcp-cache")
	}
	client := newHTTPClient(config.HTTPClient, config.APIKey)
	return &Client{config: config, http: client}, nil
}

func artifactInfo(artifact string) (extension, mime string, maximum int64, err error) {
	switch artifact {
	case "", "pdf":
		return "pdf", "application/pdf", maxPDFBytes, nil
	case "tei_xml":
		return "xml", "application/xml", maxXMLBytes, nil
	default:
		return "", "", 0, &Error{Code: "INVALID_ARGUMENT", Reason: "artifact must be pdf or tei_xml"}
	}
}

// Download rechecks local bytes before requesting content and serializes the
// same artifact across processes. Interrupted content is never range-resumed.
func (c *Client) Download(ctx context.Context, request Request) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.config.Directory == "" || !filepath.IsAbs(c.config.Directory) || strings.ContainsRune(c.config.Directory, 0) {
		return Result{}, &Error{Code: "CONFIG", Reason: "download directory must be an absolute path"}
	}
	if c.config.CacheDir == "" || !filepath.IsAbs(c.config.CacheDir) || strings.ContainsRune(c.config.CacheDir, 0) {
		return Result{}, &Error{Code: "CONFIG", Reason: "cache directory must be an absolute path"}
	}
	if request.Key == "" {
		return Result{}, &Error{Code: "INVALID_ARGUMENT", Reason: "artifact cache key is required"}
	}
	extension, mime, maximum, err := artifactInfo(request.Artifact)
	if err != nil {
		return Result{}, err
	}
	if err := validateURL(request.URL, request.Hosted); err != nil {
		return Result{}, err
	}
	if request.Hosted && strings.TrimSpace(c.config.APIKey) == "" {
		return Result{}, &Error{Code: "CONFIG", Reason: "OPENALEX_API_KEY is required for hosted content"}
	}
	if err := os.MkdirAll(c.config.Directory, 0o755); err != nil {
		return Result{}, localError(err)
	}
	if err := os.MkdirAll(c.config.CacheDir, 0o700); err != nil {
		return Result{}, localError(err)
	}
	unlock, owned, err := c.lock(ctx, request.Key)
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	if result, ok := c.Cached(ctx, request.Key); ok {
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, request.URL, nil)
	if err != nil {
		return Result{}, &Error{Code: "INVALID_ARGUMENT", Reason: "invalid artifact URL"}
	}
	if request.Hosted {
		req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, remoteError(ctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		code, reason := responseFailure(resp.StatusCode)
		return Result{}, &Error{Code: code, Reason: reason}
	}
	if resp.ContentLength > maximum {
		return Result{}, &Error{Code: "UPSTREAM", Reason: "artifact exceeds the maximum download size"}
	}
	reader := bufio.NewReader(resp.Body)
	if extension == "pdf" {
		prefix, err := reader.Peek(5)
		if err != nil || string(prefix) != "%PDF-" {
			return Result{}, &Error{Code: "UPSTREAM", Reason: "download did not contain a PDF"}
		}
	}
	temp, err := os.CreateTemp(c.config.Directory, ".annas-mcp-download-*.part")
	if err != nil {
		return Result{}, localError(err)
	}
	defer func() { temp.Close(); os.Remove(temp.Name()) }()
	hasher := sha256.New()
	written, err := copyArtifact(ctx, io.MultiWriter(temp, hasher), reader, maximum, resp.ContentLength, request.Progress)
	if err != nil {
		return Result{}, err
	}
	if resp.ContentLength >= 0 && written != resp.ContentLength {
		return Result{}, &Error{Code: "UPSTREAM", Reason: "downloaded artifact was truncated"}
	}
	if written == 0 {
		return Result{}, &Error{Code: "UPSTREAM", Reason: "download returned an empty file"}
	}
	if extension == "xml" {
		if _, err := temp.Seek(0, io.SeekStart); err != nil {
			return Result{}, localError(err)
		}
		if err := validateXML(temp); err != nil {
			return Result{}, err
		}
	}
	if err := temp.Sync(); err != nil {
		return Result{}, localError(err)
	}
	if err := temp.Chmod(0o644); err != nil {
		return Result{}, localError(err)
	}
	if err := temp.Close(); err != nil {
		return Result{}, localError(err)
	}
	if !owned() {
		return Result{}, &Error{Code: "UPSTREAM", Reason: "artifact download lease was lost; retry the request"}
	}
	filename, err := finalize(ctx, temp.Name(), c.config.Directory, request.Title, extension, request.Key)
	if err != nil {
		return Result{}, err
	}
	result := Result{Path: filename, Bytes: written, SHA256: hex.EncodeToString(hasher.Sum(nil)), MIMEType: mime}
	// Once the file exists successfully, an index write failure must not hide it.
	c.save(request.Key, result)
	return result, nil
}

func copyArtifact(ctx context.Context, destination io.Writer, source io.Reader, maximum, total int64, progress func(int64, int64)) (int64, error) {
	var accumulated, reported int64
	written, err := Copy(ctx, destination, source, maximum, func(count int) {
		accumulated += int64(count)
		if progress != nil && accumulated-reported >= 512*1024 {
			progress(accumulated, max(total, 0))
			reported = accumulated
		}
	})
	if err != nil {
		var writeError *WriteError
		if errors.As(err, &writeError) {
			return written, localError(writeError.Err)
		}
		var sourceError *ReadError
		if errors.As(err, &sourceError) {
			return written, remoteError(ctx, sourceError.Err)
		}
		return written, err
	}
	if progress != nil {
		progress(written, max(total, 0))
	}
	return written, nil
}

func validateXML(reader io.Reader) error {
	decoder := xml.NewDecoder(reader)
	root, hasText, roots := "", false, 0
	depth, teiDepth := 0, 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return &Error{Code: "UPSTREAM", Reason: "download did not contain valid structured XML"}
		}
		switch value := token.(type) {
		case xml.Directive:
			return &Error{Code: "UPSTREAM", Reason: "structured XML must not contain a document type declaration"}
		case xml.StartElement:
			if depth == 0 {
				roots++
				if root == "" {
					root = strings.ToLower(value.Name.Local)
				}
			}
			depth++
			if depth > 256 {
				return &Error{Code: "UPSTREAM", Reason: "structured XML nesting limit exceeded"}
			}
			name := strings.ToLower(value.Name.Local)
			if name == "tei" && teiDepth == 0 {
				teiDepth = depth
			}
			if name == "text" && teiDepth > 0 {
				hasText = true
			}
		case xml.EndElement:
			if depth == teiDepth {
				teiDepth = 0
			}
			depth--
		}
	}
	if roots != 1 || (root != "tei" && root != "html") || !hasText {
		return &Error{Code: "UPSTREAM", Reason: "download did not contain TEI full text"}
	}
	return nil
}

func localError(err error) error {
	return &Error{Code: "LOCAL_IO", Reason: "could not write or read the local artifact", Err: err}
}
func remoteError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return &Error{Code: "UPSTREAM", Reason: "artifact transfer failed", Err: err}
}

func responseFailure(code int) (string, string) {
	switch code {
	case http.StatusUnauthorized:
		return "CONFIG", "OpenAlex rejected the content API key"
	case http.StatusForbidden:
		return "UPSTREAM_BLOCKED", "artifact host refused access"
	case http.StatusNotFound:
		return "NOT_FOUND", "artifact is no longer available"
	case http.StatusTooManyRequests:
		return "UPSTREAM", "artifact host rate or budget limit reached"
	default:
		return "UPSTREAM", fmt.Sprintf("artifact host returned HTTP %d", code)
	}
}

type cacheEntry struct {
	Key    string `json:"key"`
	File   string `json:"file"`
	Result Result `json:"result"`
}

func (c *Client) entryPath(key string) string {
	digest := sha256.Sum256([]byte(c.config.Directory + "\x00" + key))
	return filepath.Join(c.config.CacheDir, hex.EncodeToString(digest[:])+".json")
}

// Cached returns only a regular file whose current full digest matches the
// committed cache entry. It never follows a cached symlink out of the root.
func (c *Client) Cached(ctx context.Context, key string) (Result, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.config.Directory == "" || !filepath.IsAbs(c.config.Directory) || key == "" {
		return Result{}, false
	}
	index, err := os.Open(c.entryPath(key))
	if err != nil {
		return Result{}, false
	}
	defer index.Close()
	data, err := io.ReadAll(io.LimitReader(index, 16*1024+1))
	if err != nil || len(data) > 16*1024 {
		return Result{}, false
	}
	var entry cacheEntry
	if json.Unmarshal(data, &entry) != nil || entry.Key != key || filepath.Base(entry.File) != entry.File || entry.File == "." {
		return Result{}, false
	}
	filename := filepath.Join(c.config.Directory, entry.File)
	info, err := os.Lstat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Size() != entry.Result.Bytes {
		return Result{}, false
	}
	file, err := os.Open(filename)
	if err != nil {
		return Result{}, false
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return Result{}, false
	}
	hasher := sha256.New()
	if _, err := copyArtifact(ctx, hasher, file, maxPDFBytes, info.Size(), nil); err != nil {
		return Result{}, false
	}
	if hex.EncodeToString(hasher.Sum(nil)) != entry.Result.SHA256 {
		return Result{}, false
	}
	entry.Result.Path, entry.Result.Reused = filename, true
	return entry.Result, true
}

func (c *Client) save(key string, result Result) {
	data, err := json.Marshal(cacheEntry{Key: key, File: filepath.Base(result.Path), Result: result})
	if err != nil {
		return
	}
	file, err := os.CreateTemp(c.config.CacheDir, ".artifact-*.tmp")
	if err != nil {
		return
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return
	}
	if err := file.Close(); err != nil {
		return
	}
	_ = os.Rename(file.Name(), c.entryPath(key))
}

// A unique marker identifies the owner even when the filesystem immediately
// recycles the previous directory's inode. Publish the directory and marker
// together so another process never sees a partially acquired lease.
func (c *Client) lock(ctx context.Context, key string) (func(), func() bool, error) {
	lock := c.entryPath(key) + ".lock"
	pending, err := os.MkdirTemp(filepath.Dir(lock), ".pending-lease-")
	if err != nil {
		return nil, nil, localError(err)
	}
	defer os.Remove(pending)
	marker := "owner-" + rand.Text()
	prepared := filepath.Join(pending, marker)
	if err := os.WriteFile(prepared, nil, 0o600); err != nil {
		return nil, nil, localError(err)
	}
	defer os.Remove(prepared)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if err := os.Rename(pending, lock); err == nil {
			unlock, owned := maintainLease(lock, marker)
			return unlock, owned, nil
		} else if info, statErr := os.Lstat(lock); statErr == nil && !info.IsDir() {
			return nil, nil, localError(err)
		} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return nil, nil, localError(statErr)
		} else if statErr != nil && !errors.Is(err, os.ErrExist) && !errors.Is(err, os.ErrPermission) {
			return nil, nil, localError(err)
		}
		expireLease(lock)
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func maintainLease(lock, marker string) (func(), func() bool) {
	owner := filepath.Join(lock, marker)
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		beat := time.NewTicker(15 * time.Second)
		defer beat.Stop()
		for {
			select {
			case <-done:
				return
			case now := <-beat.C:
				if err := os.Chtimes(owner, now, now); err != nil {
					return
				}
			}
		}
	}()
	owned := func() bool {
		info, err := os.Lstat(owner)
		return err == nil && info.Mode().IsRegular()
	}
	return func() {
		close(done)
		<-stopped
		if os.Remove(owner) == nil {
			_ = os.Remove(lock) // Only an empty directory can be removed.
		}
	}, owned
}

func expireLease(lock string) {
	entries, err := os.ReadDir(lock)
	if err != nil {
		return
	}
	if len(entries) == 1 && strings.HasPrefix(entries[0].Name(), "owner-") {
		owner := filepath.Join(lock, entries[0].Name())
		info, err := os.Lstat(owner)
		if err != nil || time.Since(info.ModTime()) <= 2*time.Minute || os.Remove(owner) != nil {
			return
		}
	} else if len(entries) != 0 {
		return
	} else if info, err := os.Lstat(lock); err != nil || time.Since(info.ModTime()) <= 2*time.Minute {
		return
	}
	_ = os.Remove(lock)
}
