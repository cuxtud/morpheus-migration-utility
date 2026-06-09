package morpheus

import (
	"encoding/json"
	"testing"
)

func TestDiscoveryItemName(t *testing.T) {
	tests := []struct {
		name     string
		typeHint string
		raw      string
		want     string
	}{
		{
			name:     "cypher itemKey",
			typeHint: "cypher",
			raw:      `{"id":32,"itemKey":"my-secret","name":""}`,
			want:     "my-secret",
		},
		{
			name:     "cypher key fallback",
			typeHint: "cypher",
			raw:      `{"id":1,"key":"cypher-key"}`,
			want:     "cypher-key",
		},
		{
			name:     "cloud zone code",
			typeHint: "cloud",
			raw:      `{"id":5,"code":"aws-east","zoneType":{"name":"Amazon"}}`,
			want:     "aws-east",
		},
		{
			name:     "cloud zoneType name",
			typeHint: "cloud",
			raw:      `{"id":5,"zoneType":{"name":"VMware vCenter"}}`,
			want:     "VMware vCenter",
		},
		{
			name:     "catalog item technology",
			typeHint: "catalogItem",
			raw:      `{"id":9,"technology":"apache"}`,
			want:     "apache",
		},
		{
			name:     "instance type code",
			typeHint: "instanceType",
			raw:      `{"id":3,"code":"ubuntu"}`,
			want:     "ubuntu",
		},
		{
			name:     "layout code",
			typeHint: "layout",
			raw:      `{"id":7,"layoutCode":"single"}`,
			want:     "single",
		},
		{
			name:     "node type shortName",
			typeHint: "nodeType",
			raw:      `{"id":2,"shortName":"linux-node"}`,
			want:     "linux-node",
		},
		{
			name:     "generic name",
			typeHint: "workflow",
			raw:      `{"id":1,"name":"Provision VM"}`,
			want:     "Provision VM",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := discoveryItemName(json.RawMessage(tt.raw), tt.typeHint)
			if got != tt.want {
				t.Fatalf("discoveryItemName() = %q, want %q", got, tt.want)
			}
		})
	}
}
