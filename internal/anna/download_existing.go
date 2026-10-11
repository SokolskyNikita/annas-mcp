package anna

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

// reuseOrDownload checks actual file contents, including files saved by older
// runs or renamed since downloading. A local hit needs no account request, so
// it returns no downloads_left value.
func (c *Client) reuseOrDownload(ctx context.Context, hash string, download func() (DownloadResult, error)) (DownloadResult, error) {
	if hash == "" {
		return download()
	}
	release, err := c.lockDownload(ctx, hash)
	if err != nil {
		return DownloadResult{}, err
	}
	defer release()

	result, err := findExistingDownload(ctx, c.config.DownloadPath, hash)
	if err != nil || result.Path != "" {
		return result, err
	}
	return download()
}

// lockDownload keeps a second request for this hash from downloading while the
// first is still writing its temporary file. Other hashes can run in parallel.
func (c *Client) lockDownload(ctx context.Context, hash string) (func(), error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.downloadMu.Lock()
		pending := c.downloads[hash]
		if pending == nil {
			if c.downloads == nil {
				c.downloads = make(map[string]chan struct{})
			}
			pending = make(chan struct{})
			c.downloads[hash] = pending
			c.downloadMu.Unlock()
			return func() {
				c.downloadMu.Lock()
				delete(c.downloads, hash)
				close(pending)
				c.downloadMu.Unlock()
			}, nil
		}
		c.downloadMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-pending:
		}
	}
}

func findExistingDownload(ctx context.Context, folder, hash string) (DownloadResult, error) {
	if err := ctx.Err(); err != nil {
		return DownloadResult{}, err
	}
	entries, err := os.ReadDir(folder)
	if os.IsNotExist(err) {
		return DownloadResult{}, nil
	}
	if err != nil {
		return DownloadResult{}, apperr.Wrap(apperr.Config, "cannot read the download folder "+folder, err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return DownloadResult{}, err
		}
		// Never return an in-progress download, a symlink or a directory.
		partial := strings.HasPrefix(entry.Name(), ".annas-mcp-download-") && strings.HasSuffix(entry.Name(), ".part")
		if !entry.Type().IsRegular() || partial {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxDownloadBytes {
			continue
		}
		path := filepath.Join(folder, entry.Name())
		if matchesLocalHash(ctx, path, info, hash) {
			return DownloadResult{Path: path, Bytes: info.Size()}, nil
		}
	}
	return DownloadResult{}, ctx.Err()
}

func matchesLocalHash(ctx context.Context, path string, before os.FileInfo, hash string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return false
	}
	hasher := md5.New()
	// Bound the read in case another process is appending to the file.
	written, err := io.Copy(hasher, io.LimitReader(localHashReader{ctx, file}, before.Size()+1))
	if err != nil || written != before.Size() || !strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), hash) {
		return false
	}
	after, err := os.Lstat(path)
	return err == nil && ctx.Err() == nil && after.Mode().IsRegular() &&
		os.SameFile(before, after) && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime())
}

type localHashReader struct {
	ctx    context.Context
	source io.Reader
}

func (r localHashReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(p)
}
