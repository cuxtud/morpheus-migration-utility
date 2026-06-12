package migrate

import (
	"encoding/json"
	"testing"
)

func TestValidateOptionListAuth_apiAndManual(t *testing.T) {
	for _, typ := range []string{"api", "manual"} {
		obj := map[string]interface{}{
			"name": "Layouts",
			"type": typ,
		}
		if err := validateOptionListAuthForMigrate(nil, nil, obj); err != nil {
			t.Fatalf("type %s: %v", typ, err)
		}
	}
}

func TestValidateOptionListAuth_restHeaderAuthAllowed(t *testing.T) {
	obj := map[string]interface{}{
		"name": "API Instance list",
		"type": "rest",
		"config": map[string]interface{}{
			"sourceHeaders": []interface{}{
				map[string]interface{}{"name": "Authorization", "value": "BEARER x"},
			},
		},
	}
	if err := validateOptionListAuthForMigrate(nil, nil, obj); err != nil {
		t.Fatalf("header-auth REST list should migrate: %v", err)
	}
}

func TestBuildOptionTypeListWritePayload_restPreservesHeaders(t *testing.T) {
	srcObj := map[string]interface{}{
		"name":       "API Instance list",
		"type":       "rest",
		"sourceUrl":  "https://10.32.20.40/api/instances",
		"sourceMethod": "GET",
		"config": map[string]interface{}{
			"sourceHeaders": []interface{}{
				map[string]interface{}{
					"id":     float64(99),
					"name":   "Authorization",
					"value":  "BEARER secret-token",
					"masked": false,
				},
				map[string]interface{}{
					"name":   "Content-Type",
					"value":  "application/json",
					"masked": false,
				},
			},
		},
	}
	payload, err := buildOptionTypeListWritePayload(nil, nil, srcObj)
	if err != nil {
		t.Fatal(err)
	}
	var wrap map[string]map[string]interface{}
	if err := json.Unmarshal(payload, &wrap); err != nil {
		t.Fatal(err)
	}
	cfg := wrap["optionTypeList"]["config"].(map[string]interface{})
	headers := cfg["sourceHeaders"].([]interface{})
	if len(headers) != 2 {
		t.Fatalf("headers len %d", len(headers))
	}
	first := headers[0].(map[string]interface{})
	if first["name"] != "Authorization" || first["value"] != "BEARER secret-token" {
		t.Fatalf("unexpected first header: %#v", first)
	}
	if first["masked"] != "off" {
		t.Fatalf("masked=%#v", first["masked"])
	}
	if _, ok := first["id"]; ok {
		t.Fatal("source header id should be stripped")
	}
}

func TestResolveOptionListCredentialName_fromEmbeddedName(t *testing.T) {
	obj := map[string]interface{}{
		"credential": map[string]interface{}{"name": "api-user"},
	}
	name, err := resolveOptionListCredentialName(nil, obj)
	if err != nil || name != "api-user" {
		t.Fatalf("got name=%q err=%v", name, err)
	}
}

func TestValidateOptionListAuth_restPublicAllowed(t *testing.T) {
	obj := map[string]interface{}{
		"name":      "Public API",
		"type":      "rest",
		"sourceUrl": "https://example.com/data",
	}
	if err := validateOptionListAuthForMigrate(nil, nil, obj); err != nil {
		t.Fatalf("public REST list should migrate: %v", err)
	}
}

func TestExtractOptionListCredentialRef(t *testing.T) {
	obj := map[string]interface{}{
		"credential": map[string]interface{}{"id": float64(9), "name": "bind-user"},
	}
	id, name, ok := extractOptionListCredentialRef(obj)
	if !ok || id != 9 || name != "bind-user" {
		t.Fatalf("got id=%d name=%q ok=%v", id, name, ok)
	}
}

func TestBuildOptionTypeListWritePayload_manualKeepsDataset(t *testing.T) {
	srcObj := map[string]interface{}{
		"id":             float64(33),
		"name":           "Location VMware All",
		"type":           "manual",
		"initialDataset": `[{"name":"A","value":"a"}]`,
		"account":        map[string]interface{}{"id": float64(1)},
	}
	payload, err := buildOptionTypeListWritePayload(nil, nil, srcObj)
	if err != nil {
		t.Fatal(err)
	}
	var wrap map[string]map[string]interface{}
	if err := json.Unmarshal(payload, &wrap); err != nil {
		t.Fatal(err)
	}
	ol := wrap["optionTypeList"]
	if ol["initialDataset"] == nil {
		t.Fatal("expected initialDataset to be preserved")
	}
	if _, ok := ol["account"]; ok {
		t.Fatal("account should be stripped")
	}
}
