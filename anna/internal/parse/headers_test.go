package parse

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// headerCases are the download headers in testdata/cases/headers.json.
type headerCases struct {
	Comment string
	Formats []struct {
		Name, URL, Format string
		Headers           http.Header
	}
	ContentRanges struct {
		Valid []struct {
			Value string
			Start int64
		}
		Invalid []string
	} `json:"content_ranges"`
	HTML []struct {
		ContentType string `json:"content_type"`
		Body        string
		HTML        bool
	}
	MediaTypeExtensions []struct {
		ContentType string `json:"content_type"`
		Extension   string
	} `json:"media_type_extensions"`
}

func loadHeaderCases(t *testing.T) headerCases {
	t.Helper()
	var cases headerCases
	loadCases(t, "headers", &cases)
	return cases
}

func TestDownloadFormatUsesFilenameMediaTypeAndURL(t *testing.T) {
	t.Parallel()

	for _, tc := range loadHeaderCases(t).Formats {
		response := &http.Response{Header: tc.Headers}
		if tc.URL != "" {
			response.Request = httptest.NewRequest(http.MethodGet, tc.URL, nil)
		}
		if got := DownloadFormat(response.Header, requestURLOf(response)); got != tc.Format {
			t.Errorf("%s: format = %q, want %q", tc.Name, got, tc.Format)
		}
	}
}

func requestURLOf(response *http.Response) *url.URL {
	if response.Request == nil {
		return nil
	}
	return response.Request.URL
}

func TestContentRangeStart(t *testing.T) {
	t.Parallel()

	ranges := loadHeaderCases(t).ContentRanges
	for _, tc := range ranges.Valid {
		if got, ok := ContentRangeStart(tc.Value); !ok || got != tc.Start {
			t.Errorf("ContentRangeStart(%q) = %d, %v", tc.Value, got, ok)
		}
	}
	for _, value := range ranges.Invalid {
		if _, ok := ContentRangeStart(value); ok {
			t.Errorf("ContentRangeStart(%q) accepted a malformed range", value)
		}
	}
}

func TestLooksLikeHTML(t *testing.T) {
	t.Parallel()

	for _, tc := range loadHeaderCases(t).HTML {
		if got := LooksLikeHTML(tc.ContentType, []byte(tc.Body)); got != tc.HTML {
			t.Errorf("LooksLikeHTML(%q, %q) = %v", tc.ContentType, tc.Body, got)
		}
	}
}

func TestExtensionFromMediaType(t *testing.T) {
	t.Parallel()

	for _, tc := range loadHeaderCases(t).MediaTypeExtensions {
		if got := extensionFromMediaType(tc.ContentType); got != tc.Extension {
			t.Errorf("extensionFromMediaType(%q) = %q, want %q", tc.ContentType, got, tc.Extension)
		}
	}
}
