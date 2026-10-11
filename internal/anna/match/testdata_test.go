package match

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// loadCases reads testdata/cases/<name>.json into cases. Unknown fields are
// an error, so a misspelled field in a case cannot pass unnoticed.
func loadCases(t testing.TB, name string, cases any) {
	t.Helper()
	file := filepath.Join("testdata", "cases", name+".json")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(cases); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
}
