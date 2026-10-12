package parse

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestLooseJSONValuesTolerateNumbersAndNulls(t *testing.T) {
	t.Parallel()

	var record Record
	if err := json.Unmarshal([]byte(snippet(t, "record_loose_values.json")), &record); err != nil {
		t.Fatal(err)
	}
	file := record.FileUnifiedData
	if file.YearBest != "2008" || file.FilesizeBest != "12" || file.HasAADownloads != "true" || file.TitleBest != "" ||
		!reflect.DeepEqual([]string(file.TitleAdditional), []string{"a", "", "3"}) ||
		!reflect.DeepEqual([]string(file.AuthorAdditional), []string{"one"}) ||
		!reflect.DeepEqual([]string(file.IdentifiersUnified["isbn13"]), []string{"9780000000002"}) {
		t.Fatalf("record = %+v", file)
	}
	for _, bad := range loadJSONCases(t).MalformedRecords {
		if json.Unmarshal([]byte(bad), &record) == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestIsUpAnswer(t *testing.T) {
	t.Parallel()

	for _, tc := range loadJSONCases(t).UpAnswers {
		if got := IsUpAnswer([]byte(tc.Body)); got != tc.Up {
			t.Errorf("IsUpAnswer(%q) = %v, want %v", tc.Body, got, tc.Up)
		}
	}
}

func TestMD5InlineInfoRequiresPublicCounters(t *testing.T) {
	t.Parallel()

	var info MD5InlineInfo
	if err := json.Unmarshal(readInlineInfoFixture(t, "md5_inline_info.json"), &info); err != nil {
		t.Fatal(err)
	}
	want := MD5InlineInfo{ReportsCount: 2, CommentsCount: 0, ListsCount: 4, DownloadsTotal: 345, GreatQualityCount: 1}
	if info != want {
		t.Fatalf("statistics = %+v, want %+v", info, want)
	}
	var zero MD5InlineInfo
	if err := json.Unmarshal(readInlineInfoFixture(t, "md5_inline_info_zero.json"), &zero); err != nil {
		t.Fatalf("zero counters: %v", err)
	}
	if zero != (MD5InlineInfo{}) {
		t.Fatalf("zero counters = %+v", zero)
	}

	valid := string(readInlineInfoFixture(t, "md5_inline_info.json"))
	for _, value := range loadInlineInfoCases(t).InvalidCounterValues {
		body := strings.Replace(valid, `"reports_count": 2`, `"reports_count": `+value, 1)
		var bad MD5InlineInfo
		if err := json.Unmarshal([]byte(body), &bad); err == nil {
			t.Errorf("accepted invalid reports_count %s", value)
		}
	}
	var missing MD5InlineInfo
	if err := json.Unmarshal([]byte(`{}`), &missing); err == nil {
		t.Error("accepted response without required counters")
	}
}

// jsonCases are the JSON answers in testdata/cases/json.json.
type jsonCases struct {
	Comment          string
	MalformedRecords []string `json:"malformed_records"`
	UpAnswers        []struct {
		Body string
		Up   bool
	} `json:"up_answers"`
}

type inlineInfoCases struct {
	Comment              string
	InvalidCounterValues []string `json:"invalid_counter_values"`
}

func loadInlineInfoCases(t *testing.T) inlineInfoCases {
	t.Helper()
	var cases inlineInfoCases
	loadCases(t, "md5_inline_info", &cases)
	return cases
}

func readInlineInfoFixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := readTestdata(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

func loadJSONCases(t *testing.T) jsonCases {
	t.Helper()
	var cases jsonCases
	loadCases(t, "json", &cases)
	return cases
}

func TestParseCitationReadsCapturedDOIData(t *testing.T) {
	t.Parallel()

	citation, err := ParseCitation(strings.NewReader(string(readPage(t, "csl_adam.json"))))
	if err != nil {
		t.Fatal(err)
	}
	want := Citation{DOI: "10.48550/ARXIV.1412.6980", Title: "Adam: A Method for Stochastic Optimization", Authors: []string{"Diederik P. Kingma", "Jimmy Ba"}, Year: "2014"}
	if !reflect.DeepEqual(citation, want) {
		t.Fatalf("citation = %+v", citation)
	}
}

func TestParseCitationReadsEveryShape(t *testing.T) {
	t.Parallel()

	citation, err := ParseCitation(strings.NewReader(snippet(t, "csl_every_shape.json")))
	if err != nil {
		t.Fatal(err)
	}
	if citation.DOI != "10.1000/x" || citation.Title != "A title" || !reflect.DeepEqual(citation.Authors, []string{"A Consortium", "Ada Lovelace"}) || citation.Year != "1843" {
		t.Fatalf("citation = %+v", citation)
	}
	if _, err := ParseCitation(strings.NewReader(snippet(t, "csl_without_title.json"))); err == nil {
		t.Fatal("a citation without a title was accepted")
	}
	if _, err := ParseCitation(strings.NewReader(`not json`)); err == nil {
		t.Fatal("malformed JSON was accepted")
	}
	if citation, _ := ParseCitation(strings.NewReader(snippet(t, "csl_malformed_parts.json"))); citation.Authors != nil || citation.Year != "" {
		t.Fatalf("malformed authors or date were read: %+v", citation)
	}
}

func TestLooseJSONValuesRejectMalformedStrings(t *testing.T) {
	t.Parallel()

	var value LooseString
	if err := value.UnmarshalJSON([]byte(`"\q"`)); err == nil {
		t.Error("an invalid string escape was accepted")
	}
	var values LooseStrings
	if err := values.UnmarshalJSON([]byte(`"\q"`)); err == nil {
		t.Error("an invalid single value was accepted")
	}
	if err := values.UnmarshalJSON([]byte(`null`)); err != nil || values != nil {
		t.Errorf("null: %v %v", values, err)
	}
}
