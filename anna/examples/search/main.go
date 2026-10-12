// Search is an explicitly invoked, credentialed example: go run ./anna/examples/search.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/SokolskyNikita/annas-mcp/anna"
)

func main() {
	client, err := anna.NewClient(anna.Config{SecretKey: os.Getenv("ANNAS_SECRET_KEY"), AutoBaseURL: true})
	if err != nil {
		log.Fatal(err)
	}
	page, err := client.FindBook(context.Background(), "computing", anna.SearchOptions{Language: []string{"en"}}, time.Minute)
	if err != nil {
		var detail *anna.Error
		if errors.As(err, &detail) {
			log.Printf("field=%s", detail.Field)
		}
		log.Fatalf("%s: %v", anna.CodeOf(err), err)
	}
	fmt.Printf("%d exact, %d partial; total=%d lower-bound=%t more-pages=%t\n", len(page.Results), len(page.Partial), page.Total, page.TotalIsLowerBound, page.MorePages)
	for _, book := range page.Results {
		fmt.Printf("%s — %s (%s)\n", book.Title, book.Authors, book.Hash)
	}
}
