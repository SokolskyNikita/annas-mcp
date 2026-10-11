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
	body := `{"id":"md5:x","file_unified_data":{"year_best":2008,"filesize_best":12,"has_aa_downloads":true,"title_best":null,"title_additional":["a",null,3],"author_additional":"one","identifiers_unified":{"isbn13":"9780000000002"}}}`
	if err := json.Unmarshal([]byte(body), &record); err != nil {
		t.Fatal(err)
	}
	file := record.FileUnifiedData
	if file.YearBest != "2008" || file.FilesizeBest != "12" || file.HasAADownloads != "true" || file.TitleBest != "" ||
		!reflect.DeepEqual([]string(file.TitleAdditional), []string{"a", "", "3"}) ||
		!reflect.DeepEqual([]string(file.AuthorAdditional), []string{"one"}) ||
		!reflect.DeepEqual([]string(file.IdentifiersUnified["isbn13"]), []string{"9780000000002"}) {
		t.Fatalf("record = %+v", file)
	}
	for _, bad := range []string{`{"file_unified_data":{"title_best":"unterminated}}`, `{"file_unified_data":{"title_additional":["a",}}`} {
		if json.Unmarshal([]byte(bad), &record) == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestIsUpAnswer(t *testing.T) {
	t.Parallel()

	for body, want := range map[string]bool{
		`{"aa_logged_in":0}`: true,
		`{"aa_logged_in":1}`: true,
		`{"other":1}`:        false,
		`<html>check</html>`: false,
		``:                   false,
	} {
		if got := IsUpAnswer([]byte(body)); got != want {
			t.Errorf("IsUpAnswer(%q) = %v, want %v", body, got, want)
		}
	}
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

	citation, err := ParseCitation(strings.NewReader(`{"doi":"10.1000/x","title":["A title","Subtitle"],"author":[{"literal":"A Consortium"},{"given":"Ada","family":"Lovelace"},{}],"published-print":{"date-parts":[[1843,10]]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if citation.DOI != "10.1000/x" || citation.Title != "A title" || !reflect.DeepEqual(citation.Authors, []string{"A Consortium", "Ada Lovelace"}) || citation.Year != "1843" {
		t.Fatalf("citation = %+v", citation)
	}
	if _, err := ParseCitation(strings.NewReader(`{"DOI":"10.1000/x"}`)); err == nil {
		t.Fatal("a citation without a title was accepted")
	}
	if _, err := ParseCitation(strings.NewReader(`not json`)); err == nil {
		t.Fatal("malformed JSON was accepted")
	}
	if citation, _ := ParseCitation(strings.NewReader(`{"title":"T","author":"not a list","issued":{"date-parts":[[0]]}}`)); citation.Authors != nil || citation.Year != "" {
		t.Fatalf("malformed authors or date were read: %+v", citation)
	}
}
