package migrate

import "testing"

func TestExtractCloudCredentialRef(t *testing.T) {
	zone := map[string]interface{}{
		"credential": map[string]interface{}{"id": float64(5), "name": "vcenter-cred"},
	}
	id, name := extractCloudCredentialRef(zone)
	if id != 5 || name != "vcenter-cred" {
		t.Fatalf("credential ref: id=%d name=%q", id, name)
	}

	zone = map[string]interface{}{
		"config": map[string]interface{}{"credentialId": float64(9)},
	}
	id, name = extractCloudCredentialRef(zone)
	if id != 9 || name != "" {
		t.Fatalf("config credentialId: id=%d name=%q", id, name)
	}
}

func TestExtractGroupCloudNames(t *testing.T) {
	group := map[string]interface{}{
		"zones": []interface{}{
			map[string]interface{}{"id": float64(1), "name": "devnverVMware"},
			map[string]interface{}{"id": float64(2), "name": "AWS East"},
		},
	}
	names := extractGroupCloudNames(group)
	if len(names) != 2 || names[0] != "devnverVMware" || names[1] != "AWS East" {
		t.Fatalf("names=%v", names)
	}
}

func TestItemTypeOrder_infraBeforeCatalog(t *testing.T) {
	if itemTypeOrder("credential") >= itemTypeOrder("group") {
		t.Fatal("credential should sort before group")
	}
	if itemTypeOrder("group") >= itemTypeOrder("cloud") {
		t.Fatal("group should sort before cloud")
	}
	if itemTypeOrder("cloud") >= itemTypeOrder("catalogItem") {
		t.Fatal("cloud should sort before catalog item")
	}
}
