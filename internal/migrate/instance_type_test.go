package migrate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProvisionTypeCodeFrom(t *testing.T) {
	obj := map[string]interface{}{
		"provisionType": map[string]interface{}{"code": "vmware"},
	}
	if got := provisionTypeCodeFrom(obj); got != "vmware" {
		t.Fatalf("got %q", got)
	}
	obj = map[string]interface{}{"provisionTypeCode": "aws"}
	if got := provisionTypeCodeFrom(obj); got != "aws" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatMemoryRequirement(t *testing.T) {
	if got := formatMemoryRequirement(float64(2147483648)); got != "2147483648" {
		t.Fatalf("got %v", got)
	}
	if got := formatMemoryRequirement("1024"); got != "1024" {
		t.Fatalf("got %v", got)
	}
}

func TestBuildInstanceTypeLayoutWritePayload(t *testing.T) {
	layout := map[string]interface{}{
		"name":              "rocky 9",
		"instanceVersion":   "9",
		"creatable":         true,
		"memoryRequirement": float64(2147483648),
		"provisionType":     map[string]interface{}{"code": "vmware"},
	}
	payload, err := buildInstanceTypeLayoutWritePayload(layout, []int64{2006}, []interface{}{2628}, []int64{36})
	if err != nil {
		t.Fatal(err)
	}
	var wrap map[string]map[string]interface{}
	if err := json.Unmarshal(payload, &wrap); err != nil {
		t.Fatal(err)
	}
	l := wrap["instanceTypeLayout"]
	if l["provisionTypeCode"] != "vmware" {
		t.Fatalf("provisionTypeCode: %v", l["provisionTypeCode"])
	}
	mem := stringFromAny(l["memoryRequirement"])
	if mem == "" {
		t.Fatalf("memoryRequirement missing: %#v", l)
	}
}

func TestFilterItemsForInstanceTypeOrchestration(t *testing.T) {
	items := []SelectedItem{
		{Type: "instanceType", ID: 102, RawJSON: `{"id":102,"instanceTypeLayouts":[{"id":1324},{"id":1325}]}`},
		{Type: "layout", ID: 1324, Name: "rocky 8", RawJSON: `{"id":1324,"containerTypes":[{"id":2005}],"taskSets":[{"id":36}]}`},
		{Type: "layout", ID: 1325, Name: "rocky 9"},
		{Type: "nodeType", ID: 2005, Name: "Rocky 8"},
		{Type: "workflow", ID: 36, Name: "mysql 8 Install"},
		{Type: "policy", ID: 1, Name: "p"},
	}
	got := filterItemsForInstanceTypeOrchestration(items)
	if len(got) != 2 {
		t.Fatalf("len %d, want 2: %#v", len(got), got)
	}
	if normalizeType(got[0].Type) != "instanceType" || normalizeType(got[1].Type) != "policy" {
		t.Fatalf("unexpected types: %#v", got)
	}
}

func TestBlockedVirtualImageMessage(t *testing.T) {
	msg := "node type Rocky 9 az cannot be created: virtual image Morpheus Rocky 9 20250218 was not found on destination"
	if !strings.Contains(msg, "virtual image") || !strings.Contains(msg, "not found on destination") {
		t.Fatalf("unexpected message: %s", msg)
	}
}
