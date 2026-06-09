package migrate

import (
	"encoding/json"
	"net/url"
	"testing"
)

func TestCypherAPIPath(t *testing.T) {
	tests := []struct {
		itemKey string
		want    string
	}{
		{"secret/uspllngcorepd02", "/api/cypher/secret/uspllngcorepd02"},
		{"/secret/uspllngcorepd02/", "/api/cypher/secret/uspllngcorepd02"},
		{"simple-key", "/api/cypher/simple-key"},
	}
	for _, tt := range tests {
		if got := cypherAPIPath(tt.itemKey); got != tt.want {
			t.Fatalf("cypherAPIPath(%q) = %q, want %q", tt.itemKey, got, tt.want)
		}
	}
}

func TestCypherItemKeyFromJSON(t *testing.T) {
	key, err := cypherItemKeyFromJSON(`{"id":41,"itemKey":"secret/uspllngcorepd01"}`)
	if err != nil {
		t.Fatal(err)
	}
	if key != "secret/uspllngcorepd01" {
		t.Fatalf("got %q", key)
	}
}

func TestCypherDataString(t *testing.T) {
	if got := cypherDataString(json.RawMessage(`"testvalue"`)); got != "testvalue" {
		t.Fatalf("string: %q", got)
	}
	if got := cypherDataString(json.RawMessage(`null`)); got != "" {
		t.Fatalf("null: %q", got)
	}
}

func TestWriteCypherValueQueryEncoding(t *testing.T) {
	q := url.Values{}
	q.Set("value", "testvalue")
	q.Set("type", "string")
	encoded := q.Encode()
	if encoded != "type=string&value=testvalue" && encoded != "value=testvalue&type=string" {
		t.Fatalf("unexpected query: %s", encoded)
	}
}
