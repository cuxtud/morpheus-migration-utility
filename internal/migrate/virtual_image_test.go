package migrate

import (
	"encoding/json"
	"testing"
)

func TestVirtualImageIDFromItems(t *testing.T) {
	body := []byte(`{
		"virtualImages": [
			{"id": 47714, "name": "AP-Rocky-9_2-x86_64"},
			{"id": 1, "name": "other"}
		]
	}`)
	var wrap map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrap); err != nil {
		t.Fatal(err)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(wrap["virtualImages"], &items); err != nil {
		t.Fatal(err)
	}
	id := virtualImageIDFromItems(items, "AP-Rocky-9_2-x86_64")
	if id != 47714 {
		t.Fatalf("got %d", id)
	}
}
