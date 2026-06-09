package migrate

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/cuxtud/morpheus-migration-utility/internal/morpheus"
)

func migrateCypher(src, dst *morpheus.Client, item SelectedItem) ItemResult {
	name := strings.TrimSpace(item.Name)
	if src == nil {
		return ItemResult{
			Name:    name,
			Type:    "cypher",
			Status:  "error",
			Message: "source appliance is required to read cypher values",
		}
	}

	itemKey, err := cypherItemKeyFromJSON(item.RawJSON)
	if err != nil {
		return ItemResult{Name: name, Type: "cypher", Status: "error", Message: err.Error()}
	}
	if name == "" {
		name = itemKey
	}

	value, valueType, err := readCypherValue(src, itemKey)
	if err != nil {
		return ItemResult{
			Name:    name,
			Type:    "cypher",
			Status:  "error",
			Message: fmt.Sprintf("read cypher from source: %v", err),
		}
	}

	existed, err := cypherExists(dst, itemKey)
	if err != nil {
		return ItemResult{
			Name:    name,
			Type:    "cypher",
			Status:  "error",
			Message: fmt.Sprintf("check cypher on destination: %v", err),
		}
	}

	if err := writeCypherValue(dst, itemKey, value, valueType); err != nil {
		return ItemResult{
			Name:    name,
			Type:    "cypher",
			Status:  "error",
			Message: err.Error(),
		}
	}

	outcome := "created"
	msg := "Created cypher on destination"
	if existed {
		outcome = "updated"
		msg = "Updated cypher on destination"
	}
	return ItemResult{Name: name, Type: "cypher", Status: "success", Outcome: outcome, Message: msg}
}

func cypherItemKeyFromJSON(rawJSON string) (string, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &obj); err != nil {
		return "", fmt.Errorf("invalid cypher json: %w", err)
	}
	for _, field := range []string{"itemKey", "key", "cypherKey"} {
		if key := strings.TrimSpace(stringFromAny(obj[field])); key != "" {
			return key, nil
		}
	}
	return "", fmt.Errorf("cypher itemKey not found in discovery payload")
}

func cypherAPIPath(itemKey string) string {
	key := strings.Trim(strings.TrimSpace(itemKey), "/")
	if key == "" {
		return "/api/cypher"
	}
	parts := strings.Split(key, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return "/api/cypher/" + strings.Join(parts, "/")
}

type cypherValueResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Type    string          `json:"type"`
}

func readCypherValue(client *morpheus.Client, itemKey string) (string, string, error) {
	body, err := client.GetRaw(cypherAPIPath(itemKey))
	if err != nil {
		return "", "", err
	}
	var resp cypherValueResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", "", fmt.Errorf("parse cypher response: %w", err)
	}
	valueType := strings.TrimSpace(resp.Type)
	if valueType == "" {
		valueType = "string"
	}
	return cypherDataString(resp.Data), valueType, nil
}

func cypherDataString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.TrimSpace(string(raw))
}

func cypherExists(client *morpheus.Client, itemKey string) (bool, error) {
	_, err := client.GetRaw(cypherAPIPath(itemKey))
	if err == nil {
		return true, nil
	}
	if strings.Contains(err.Error(), "HTTP 404") {
		return false, nil
	}
	return false, err
}

func writeCypherValue(client *morpheus.Client, itemKey, value, valueType string) error {
	q := url.Values{}
	q.Set("value", value)
	if strings.TrimSpace(valueType) != "" {
		q.Set("type", valueType)
	}
	path := cypherAPIPath(itemKey) + "?" + q.Encode()
	_, err := client.PostRaw(path, nil)
	return err
}
