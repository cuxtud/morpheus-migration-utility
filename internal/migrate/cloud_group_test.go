package migrate

import (
	"strings"
	"testing"
)

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

func TestStripCloudZoneWriteFields(t *testing.T) {
	zone := map[string]interface{}{
		"name":         "vcenter-east",
		"zoneTypeId":   float64(1),
		"domainName":   "corp.example.com",
		"networkDomain": map[string]interface{}{
			"id":   float64(12),
			"name": "corp.example.com",
		},
		"zoneType": map[string]interface{}{
			"id":   float64(9),
			"code": "vmware",
		},
		"config": map[string]interface{}{
			"apiUrl":          "https://vc.example.com",
			"networkDomainId": float64(12),
			"domainId":        float64(12),
			"domainName":      "corp.example.com",
			"zoneTypeId":      float64(1),
		},
	}
	stripCloudZoneWriteFields(zone)
	for _, key := range []string{"networkDomain", "zoneTypeId", "domainName"} {
		if _, ok := zone[key]; ok {
			t.Fatalf("%s should be stripped", key)
		}
	}
	zt := zone["zoneType"].(map[string]interface{})
	if _, ok := zt["id"]; ok {
		t.Fatal("zoneType.id should be stripped")
	}
	cfg := zone["config"].(map[string]interface{})
	for _, key := range []string{"networkDomainId", "domainName", "zoneTypeId"} {
		if _, ok := cfg[key]; ok {
			t.Fatalf("config.%s should be stripped", key)
		}
	}
	if cfg["apiUrl"] != "https://vc.example.com" {
		t.Fatal("unrelated config should remain")
	}
}

func TestBuildCloudWritePayload_stripsReadonlyZoneFields(t *testing.T) {
	zone := map[string]interface{}{
		"name":       "vcenter-east",
		"zoneTypeId": float64(1),
		"domainName": "corp.example.com",
		"enabled":    true,
		"zoneType": map[string]interface{}{
			"id":   float64(9),
			"code": "vmware",
		},
		"config": map[string]interface{}{
			"apiUrl":     "https://vc.example.com",
			"datacenter": "DC1",
		},
	}
	payload, err := buildCloudWritePayload(zone, 5, "3")
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
	for _, forbidden := range []string{"zoneTypeId", "domainName"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("payload must not contain %q: %s", forbidden, body)
		}
	}
}

func TestNormalizeCloudZoneForWrite_vmware(t *testing.T) {
	zone := map[string]interface{}{
		"name":    "vcenter-east",
		"enabled": true,
		"zoneType": map[string]interface{}{
			"code": "vmware",
		},
		"config": map[string]interface{}{
			"apiUrl":            "https://vc.example.com",
			"datacenter":        "DC1",
			"cluster":           "Cluster1",
			"resourcePoolId":    "",
			"hideHostSelection": false,
			"enableVnc":         true,
			"rpcMode":           "https",
		},
	}
	normalizeCloudZoneForWrite(zone, "vmware")
	if zone["enabled"] != "on" {
		t.Fatalf("enabled=%#v", zone["enabled"])
	}
	cfg := zone["config"].(map[string]interface{})
	if cfg["resourcePoolId"] != "All" {
		t.Fatalf("resourcePoolId=%#v", cfg["resourcePoolId"])
	}
	if cfg["certificateProvider"] != "internal" {
		t.Fatalf("certificateProvider=%#v", cfg["certificateProvider"])
	}
	if cfg["hideHostSelection"] != "off" || cfg["enableVnc"] != "on" {
		t.Fatalf("config toggles: %#v", cfg)
	}
}

func TestMorpheusOnOffString(t *testing.T) {
	if morpheusOnOffString(false, true) != "off" {
		t.Fatal("false should be off")
	}
	if morpheusOnOffString("true", false) != "on" {
		t.Fatal("true string should be on")
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
