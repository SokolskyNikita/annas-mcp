package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/SokolskyNikita/annas-mcp/anna"
)

type transport struct{ body []byte }

func (t transport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "archive.example" {
		return nil, fmt.Errorf("unexpected host %s", r.URL.Host)
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(bytes.NewReader(t.body)), Request: r}, nil
}
func main() {
	body, err := os.ReadFile("search.html")
	if err != nil {
		panic(err)
	}
	client, err := anna.NewClient(anna.Config{BaseURL: "archive.example", AccountCookie: "fake-cookie", HTTPClient: &http.Client{Transport: transport{body: body}}})
	if err != nil {
		panic(err)
	}
	page, err := client.FindBook(context.Background(), "example", anna.SearchOptions{}, 0)
	if err != nil {
		panic(err)
	}
	if page.Total != 9 || len(page.Results) != 9 || len(page.Partial) != 41 {
		panic("unexpected search contract")
	}
	fmt.Println("External SDK consumer passed")
}
