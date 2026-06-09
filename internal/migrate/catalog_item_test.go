package migrate

import (
	"strings"
	"testing"
)

func TestCatalogItemResult_withNotes(t *testing.T) {
	r := catalogItemResult("MySQL 8", "created", []string{
		"Group: using destination group \"Default\" (#1) (source group \"VMware\" not found)",
	})
	if r.Status != "success" {
		t.Fatalf("status=%q", r.Status)
	}
	if !strings.Contains(r.Message, "VMware") || !strings.Contains(r.Message, "Default") {
		t.Fatalf("message=%q", r.Message)
	}
}
