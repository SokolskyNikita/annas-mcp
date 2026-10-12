package anna

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPrepareDownloadFolderCreatesMissingFolders(t *testing.T) {
	t.Parallel()

	nested := filepath.Join(t.TempDir(), "annas-archive", "papers")
	if err := prepareDownloadFolder(nested); err != nil {
		t.Fatalf("prepareDownloadFolder: %v", err)
	}
	if entries, err := os.ReadDir(nested); err != nil || len(entries) != 0 {
		t.Fatalf("folder should exist and be left empty: %v, %v", entries, err)
	}
}

func TestDownloadReportsUnwritableFolderBeforeAnyRequest(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("folder permissions are not enforced here")
	}
	t.Parallel()

	locked := t.TempDir()
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	client := mustClient(Config{SecretKey: "key", DownloadPath: locked, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Errorf("unexpected request to %s", req.URL)
		return nil, errors.New("no network in this test")
	})}})

	_, err := client.DownloadBook(context.Background(), &Book{Hash: "0123456789abcdef0123456789abcdef", Title: "Book"}, 0, nil)
	if CodeOf(err) != CodeConfig || !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("unwritable folder returned %v", err)
	}
	var coded *Error
	if !errors.As(err, &coded) || coded.Field != "DownloadPath" {
		t.Fatalf("missing field context: %v", err)
	}
	for _, want := range []string{locked} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
