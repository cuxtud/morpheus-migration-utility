package morpheus

import (
	"encoding/json"
	"testing"
)

func TestExtractMetaTotal(t *testing.T) {
	wrap := map[string]json.RawMessage{
		"meta": json.RawMessage(`{"total":250,"size":100,"offset":0}`),
	}
	if got := extractMetaTotal(wrap); got != 250 {
		t.Fatalf("total=%d", got)
	}
}

func TestParseListPage(t *testing.T) {
	body := []byte(`{
		"meta": {"total": 2},
		"zones": [
			{"id":1,"name":"a"},
			{"id":2,"name":"b"}
		]
	}`)
	items, total, err := parseListPage(body, "zones")
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("total=%d len=%d", total, len(items))
	}
}

func TestSortPageResults(t *testing.T) {
	results := []pageResult{
		{page: 3, items: []json.RawMessage{json.RawMessage(`{"id":3}`)}},
		{page: 1, items: []json.RawMessage{json.RawMessage(`{"id":1}`)}},
		{page: 2, items: []json.RawMessage{json.RawMessage(`{"id":2}`)}},
	}
	sortPageResults(results)
	for i, want := range []int{1, 2, 3} {
		if results[i].page != want {
			t.Fatalf("index %d page=%d want %d", i, results[i].page, want)
		}
	}
}
