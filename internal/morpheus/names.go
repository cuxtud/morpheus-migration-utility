package morpheus

import "encoding/json"

// discoveryItemName returns the best human-readable label for a discovered item.
// Morpheus list endpoints are inconsistent: cyphers use itemKey, zones may only
// expose code, and library items often fall back to code when name is empty.
func discoveryItemName(raw json.RawMessage, typeHint string) string {
	for _, field := range []string{"name", "displayName", "label", "title"} {
		if n := extractStringField(raw, field); n != "" {
			return n
		}
	}

	switch typeHint {
	case "cypher":
		for _, field := range []string{"itemKey", "key", "cypherKey"} {
			if n := extractStringField(raw, field); n != "" {
				return n
			}
		}
	case "cloud":
		for _, field := range []string{"code", "zoneCode"} {
			if n := extractStringField(raw, field); n != "" {
				return n
			}
		}
		if n := extractNestedStringField(raw, "zoneType", "name"); n != "" {
			return n
		}
		if n := extractNestedStringField(raw, "zoneType", "code"); n != "" {
			return n
		}
	case "catalogItem":
		for _, field := range []string{"code", "technology", "refType"} {
			if n := extractStringField(raw, field); n != "" {
				return n
			}
		}
	case "instanceType", "layout", "nodeType":
		for _, field := range []string{"code", "shortName", "layoutCode"} {
			if n := extractStringField(raw, field); n != "" {
				return n
			}
		}
	case "virtualImage":
		for _, field := range []string{"fileName", "imageName", "externalId", "remoteName"} {
			if n := extractStringField(raw, field); n != "" {
				return n
			}
		}
	}

	for _, field := range []string{"username", "code", "itemKey", "key"} {
		if n := extractStringField(raw, field); n != "" {
			return n
		}
	}
	return ""
}
