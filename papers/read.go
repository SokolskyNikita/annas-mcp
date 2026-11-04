package papers

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const defaultReadCharacters = 8000
const maxReadCharacters = 16000
const maxReadXMLBytes = 32 << 20

// ReadOptions requests one bounded passage. Continuation must be replayed
// with the same ID and refers to the exact downloaded artifact, not a path.
type ReadOptions struct {
	ID           string `json:"id"`
	Continuation string `json:"continuation,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

// Passage is an extracted paragraph with its available section heading.
// Paragraph numbers identify this extraction and are not PDF page numbers.
type Passage struct {
	Section   string `json:"section,omitempty"`
	Paragraph int    `json:"paragraph"`
	Text      string `json:"text"`
}

// ReadResult identifies the source of text, including abstract-only results.
type ReadResult struct {
	OpenAlexID string       `json:"openalex_id,omitempty"`
	Title      string       `json:"title"`
	URL        string       `json:"url,omitempty"`
	Source     string       `json:"source"`
	Text       string       `json:"text"`
	Passages   []Passage    `json:"passages,omitempty"`
	Digest     string       `json:"sha256,omitempty"`
	NextArgs   *ReadOptions `json:"next_args,omitempty"`
	Warnings   []string     `json:"warnings,omitempty"`
}

type readCursor struct {
	ID        string `json:"id"`
	Digest    string `json:"digest"`
	Paragraph int    `json:"paragraph"`
	Offset    int    `json:"offset"`
}

type textParagraph struct {
	section string
	text    string
}

// extractTEI reads only paragraphs within the TEI text. Front matter,
// bibliographies, header metadata and instructions outside TEI are excluded.
// The names are case insensitive to support older HTML-wrapped GROBID TEI.
func extractTEI(ctx context.Context, source io.Reader) ([]textParagraph, error) {
	bounded := &io.LimitedReader{R: source, N: maxReadXMLBytes + 1}
	decoder := xml.NewDecoder(bounded)
	decoder.Entity = map[string]string{"nbsp": "\u00a0"}
	var stack []string
	var paragraphs []textParagraph
	var block strings.Builder
	var section string
	var blockName string
	blockDepth := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		token, err := decoder.Token()
		if bounded.N == 0 {
			return nil, errors.New("structured full text exceeds the reading size limit")
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid structured full text: %w", err)
		}
		switch value := token.(type) {
		case xml.Directive:
			return nil, errors.New("structured full text contains an unsupported XML directive")
		case xml.StartElement:
			name := strings.ToLower(value.Name.Local)
			stack = append(stack, name)
			if len(stack) > 256 {
				return nil, errors.New("structured full text exceeds the nesting limit")
			}
			if blockDepth != 0 {
				if name == "lb" || name == "br" {
					block.WriteByte(' ')
				}
				continue
			}
			if teiParagraphContext(stack) && isTextBlock(name) {
				blockDepth, blockName = len(stack), name
				block.Reset()
			}
		case xml.CharData:
			if blockDepth != 0 {
				block.Write(value)
			}
		case xml.EndElement:
			if blockDepth == len(stack) {
				text := strings.Join(strings.Fields(block.String()), " ")
				if blockName == "head" {
					section = text
				} else if text != "" {
					paragraphs = append(paragraphs, textParagraph{section: section, text: text})
				}
				blockDepth = 0
			}
			stack = stack[:len(stack)-1]
		}
	}
	return paragraphs, nil
}

func teiParagraphContext(stack []string) bool {
	inTEI, inText := false, false
	for _, name := range stack {
		switch name {
		case "tei":
			inTEI = true
		case "text":
			inText = inTEI
		case "teiheader", "front", "back":
			return false
		}
	}
	return inTEI && inText
}

func isTextBlock(name string) bool {
	switch name {
	case "p", "head", "item", "ab", "cell":
		return true
	default:
		return false
	}
}

func readArtifact(ctx context.Context, artifactPath string) ([]textParagraph, string, error) {
	file, err := os.Open(artifactPath)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() || info.Size() > maxReadXMLBytes {
		return nil, "", errors.New("structured full text is not a supported regular file")
	}
	hasher := sha256.New()
	paragraphs, err := extractTEI(ctx, io.TeeReader(file, hasher))
	if err != nil {
		return nil, "", err
	}
	return paragraphs, hex.EncodeToString(hasher.Sum(nil)), nil
}

func readPage(id, digest string, paragraphs []textParagraph, options ReadOptions) ([]Passage, string, *ReadOptions, error) {
	limit := options.Limit
	if limit == 0 {
		limit = defaultReadCharacters
	}
	if limit < 1 || limit > maxReadCharacters {
		return nil, "", nil, errors.New("limit must be between 1 and 16000 characters")
	}
	cursor := readCursor{ID: id, Digest: digest}
	if options.Continuation != "" {
		var err error
		cursor, err = decodeReadCursor(options.Continuation)
		if err != nil {
			return nil, "", nil, err
		}
		if cursor.ID != id || cursor.Digest != digest {
			return nil, "", nil, errors.New("reading continuation refers to a different work or changed artifact")
		}
	}
	if cursor.Paragraph < 0 || cursor.Paragraph > len(paragraphs) || cursor.Offset < 0 || (cursor.Paragraph == len(paragraphs) && cursor.Offset != 0) {
		return nil, "", nil, errors.New("invalid reading continuation position")
	}
	var passages []Passage
	var text strings.Builder
	remaining := limit
	for cursor.Paragraph < len(paragraphs) {
		paragraph := paragraphs[cursor.Paragraph]
		runes := []rune(paragraph.text)
		if cursor.Offset > len(runes) {
			return nil, "", nil, errors.New("invalid reading continuation offset")
		}
		if cursor.Offset == len(runes) {
			cursor.Paragraph++
			cursor.Offset = 0
			continue
		}
		separator := 0
		if len(passages) > 0 {
			separator = 2
		}
		if remaining <= separator {
			break
		}
		available := len(runes) - cursor.Offset
		// Keep an ordinary paragraph intact for the next call. An oversized
		// paragraph is the only case that needs an in-paragraph continuation.
		if len(passages) > 0 && available > remaining-separator {
			break
		}
		take := min(available, remaining-separator)
		piece := string(runes[cursor.Offset : cursor.Offset+take])
		passages = append(passages, Passage{Section: paragraph.section, Paragraph: cursor.Paragraph + 1, Text: piece})
		if separator > 0 {
			text.WriteString("\n\n")
		}
		text.WriteString(piece)
		remaining -= take + separator
		cursor.Offset += take
		if cursor.Offset < len(runes) {
			break
		}
		cursor.Paragraph++
		cursor.Offset = 0
	}
	var next *ReadOptions
	if cursor.Paragraph < len(paragraphs) {
		encoded, err := json.Marshal(cursor)
		if err != nil {
			return nil, "", nil, err
		}
		next = &ReadOptions{ID: options.ID, Limit: options.Limit, Continuation: base64.RawURLEncoding.EncodeToString(encoded)}
	}
	return passages, text.String(), next, nil
}

func decodeReadCursor(raw string) (readCursor, error) {
	var cursor readCursor
	if len(raw) > 2048 {
		return cursor, errors.New("invalid reading continuation")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.ID == "" || cursor.Digest == "" || cursor.Paragraph < 0 || cursor.Offset < 0 {
		return cursor, errors.New("invalid reading continuation")
	}
	return cursor, nil
}

// Read returns a bounded passage with source attribution. It uses OpenAlex's
// cached structured text, and falls back to a clearly labeled abstract when
// the work has no structured full text. Reading never scrapes landing pages.
func (c *Client) Read(ctx context.Context, options ReadOptions) (*ReadResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(options.ID) == "" {
		return nil, readArgumentError("id is required")
	}
	if options.Limit < 0 || options.Limit > maxReadCharacters {
		return nil, readArgumentError("limit must be between 1 and 16000 characters, or zero for the default")
	}
	var continuation readCursor
	if options.Continuation != "" {
		var err error
		continuation, err = decodeReadCursor(options.Continuation)
		if err != nil {
			return nil, readArgumentError(err.Error())
		}
	}
	work, err := c.Details(ctx, options.ID, DetailsOptions{Provider: "openalex"})
	if err != nil {
		return nil, err
	}
	if options.Continuation != "" && continuation.ID != work.OpenAlexID {
		return nil, readArgumentError("reading continuation refers to a different work")
	}
	var paragraphs []textParagraph
	var digest string
	var source string
	var warnings []string
	if work.ContentXMLURL == "" {
		if strings.TrimSpace(work.Abstract) == "" {
			return nil, unreadableWork(work, "no_structured_text", "no structured full text or abstract is available")
		}
		paragraphs = []textParagraph{{section: "Abstract", text: work.Abstract}}
		sum := sha256.Sum256([]byte(work.Abstract))
		digest = hex.EncodeToString(sum[:])
		source = "abstract"
		warnings = []string{"Only the abstract is available through this reading route."}
	} else {
		artifact, downloadErr := c.downloadCandidate(ctx, work, DownloadOptions{Artifact: "tei_xml"},
			downloadCandidate{url: work.ContentXMLURL, route: "hosted_tei_xml", hosted: true}, nil)
		if downloadErr != nil {
			return nil, c.wrap(downloadErr, "openalex")
		}
		paragraphs, digest, err = readArtifact(ctx, artifact.Path)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			failure := unreadableWork(work, "invalid_structured_text", "could not read the structured full text")
			failure.Err = err
			return nil, failure
		}
		if len(paragraphs) == 0 {
			return nil, unreadableWork(work, "empty_structured_text", "structured full text contains no readable body paragraphs")
		}
		source = "openalex_tei"
		warnings = []string{"Machine-extracted text can omit content, equations and figures. Paragraph labels are not PDF page numbers."}
	}
	passages, text, next, err := readPage(work.OpenAlexID, digest, paragraphs, options)
	if err != nil {
		return nil, readArgumentError(err.Error())
	}
	url := work.URL
	if url == "" {
		url = work.PageURL
	}
	return &ReadResult{OpenAlexID: work.OpenAlexID, Title: work.Title, URL: url,
		Source: source, Text: text, Passages: passages, Digest: digest,
		NextArgs: next, Warnings: warnings}, nil
}

func readArgumentError(message string) *Error {
	return &Error{Code: "INVALID_ARGUMENT", Message: message, Provider: "openalex", Reason: "invalid_argument"}
}

func unreadableWork(work *Work, reason, message string) *Error {
	failure := &Error{Code: "NOT_FOUND", Message: message, Provider: "openalex", Reason: reason,
		WorkID: work.OpenAlexID, LandingPage: work.PageURL}
	if len(work.DownloadArgs) != 0 {
		failure.NextActions = []map[string]any{{"tool": "article_download", "arguments": work.DownloadArgs}}
	}
	return failure
}
