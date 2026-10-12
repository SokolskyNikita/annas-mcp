// Download is explicitly invoked with an MD5 and destination; it may use account allowance.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/SokolskyNikita/annas-mcp/anna"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: go run ./anna/examples/download <md5> <directory>")
	}
	dir, err := filepath.Abs(os.Args[2])
	if err != nil {
		log.Fatal(err)
	}
	client, err := anna.NewClient(anna.Config{SecretKey: os.Getenv("ANNAS_SECRET_KEY"), DownloadPath: dir, AutoBaseURL: true})
	if err != nil {
		log.Fatal(err)
	}
	result, err := client.DownloadBook(context.Background(), &anna.Book{Hash: os.Args[1]}, 30*time.Minute, func(done, total int64) { fmt.Fprintf(os.Stderr, "\r%d / %d bytes", done, total) })
	if err != nil {
		log.Fatalf("%s: %v", anna.CodeOf(err), err)
	}
	fmt.Printf("\n%s (%d bytes)\n", result.Path, result.Bytes)
}
