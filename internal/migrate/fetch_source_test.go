package migrate

import (
	"encoding/json"
	"testing"
)

func TestUnwrapFirstJSONKey_instanceTypeLayout(t *testing.T) {
	body := []byte(`{"success":true,"instanceTypeLayout":{"id":1324,"name":"rocky 8"}}`)
	raw, key, err := unwrapFirstJSONKey(body, []string{"instanceTypeLayout", "layout"})
	if err != nil {
		t.Fatal(err)
	}
	if key != "instanceTypeLayout" {
		t.Fatalf("key %q", key)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	if intFromAny(obj["id"]) != 1324 {
		t.Fatalf("id %v", obj["id"])
	}
}
