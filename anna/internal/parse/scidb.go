package parse

import (
	"encoding/hex"
	"errors"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Why a SciDB page gives no file.
var (
	ErrSciDBEmpty    = errors.New("SciDB response was empty")
	ErrSciDBPageURL  = errors.New("invalid SciDB page URL")
	ErrSciDBNoFile   = errors.New("SciDB did not provide one usable PDF viewer link")
	ErrSciDBNoRecord = errors.New("SciDB did not identify one archive file for checksum verification")
)

// SciDB embeds its PDF in the same-origin PDF.js viewer. Only read that
// viewer's file parameter, rather than guessing from arbitrary page links.
// SciDBFile reads the PDF a SciDB page embeds and the hash of its archive
// record, which the download is checked against.
func SciDBFile(doc *goquery.Document, pageURL string) (downloadURL, hash string, err error) {
	if doc == nil {
		return "", "", ErrSciDBEmpty
	}
	page, parseErr := url.Parse(pageURL)
	if parseErr != nil || page.Scheme != "https" || page.Host == "" || page.User != nil {
		return "", "", ErrSciDBPageURL
	}
	files := make(map[string]struct{})
	doc.Find("iframe[src]").Each(func(_ int, frame *goquery.Selection) {
		src, _ := frame.Attr("src")
		ref, parseErr := url.Parse(src)
		if parseErr != nil {
			return
		}
		viewer := page.ResolveReference(ref)
		if !sameOrigin(page, viewer) || viewer.User != nil || viewer.Path != viewerPath {
			return
		}
		if file, ok := resolveFile(page, viewer.Query().Get("file")); ok {
			files[file] = struct{}{}
		}
	})
	if len(files) != 1 {
		return "", "", ErrSciDBNoFile
	}
	hashes := make(map[string]struct{})
	doc.Find("a[href^='/md5/']").Each(func(_ int, link *goquery.Selection) {
		href, _ := link.Attr("href")
		if md5, ok := md5Hash(strings.TrimPrefix(href, "/md5/")); ok {
			hashes[md5] = struct{}{}
		}
	})
	if len(hashes) != 1 {
		return "", "", ErrSciDBNoRecord
	}
	for file := range files {
		downloadURL = file
	}
	for md5 := range hashes {
		hash = md5
	}
	return downloadURL, hash, nil
}

// resolveFile resolves a file link on page. It must use HTTPS without
// credentials; a relative link must stay on the page's origin.
func resolveFile(page *url.URL, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	resolved := ref
	if !ref.IsAbs() {
		if resolved = page.ResolveReference(ref); !sameOrigin(page, resolved) {
			return "", false
		}
	}
	if resolved.Scheme != "https" || resolved.Host == "" || resolved.User != nil {
		return "", false
	}
	return resolved.String(), true
}

func sameOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

// md5Hash reads a 32-character hexadecimal MD5, in lower case.
func md5Hash(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 32 {
		return "", false
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", false
	}
	return value, true
}
