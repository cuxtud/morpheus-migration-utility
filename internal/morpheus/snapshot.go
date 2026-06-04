package morpheus

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ApplianceSnapshot is a full inventory + operational snapshot for one Morpheus appliance.
type ApplianceSnapshot struct {
	URL         string           `json:"url"`
	ConnectedAs string           `json:"connectedAs,omitempty"`
	DiscoveredAt string          `json:"discoveredAt"`
	Discovery   *DiscoveryResult `json:"discovery"`
	License     json.RawMessage  `json:"license,omitempty"`
	LicenseErr  string           `json:"licenseError,omitempty"`
	Health      json.RawMessage  `json:"health,omitempty"`
	HealthErr   string           `json:"healthError,omitempty"`
}

// DiscoverAppliance runs resource discovery plus license and health where the API allows.
func (c *Client) DiscoverAppliance() *ApplianceSnapshot {
	out := &ApplianceSnapshot{
		URL:          c.BaseURL,
		DiscoveredAt: time.Now().UTC().Format(time.RFC3339),
	}
	if user, err := c.TestConnection(); err == nil {
		out.ConnectedAs = user
	} else {
		out.ConnectedAs = ""
	}
	out.Discovery = c.Discover()
	if raw, err := c.fetchOptionalJSON("/api/license"); err != nil {
		out.LicenseErr = err.Error()
	} else {
		out.License = raw
	}
	if raw, err := c.fetchHealthJSON(); err != nil {
		out.HealthErr = err.Error()
	} else {
		out.Health = raw
	}
	return out
}

func (c *Client) fetchOptionalJSON(path string) (json.RawMessage, error) {
	body, err := c.GetRaw(path)
	if err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("invalid JSON from %s", path)
	}
	return json.RawMessage(body), nil
}

var errHealthNotAvailable = errors.New("health endpoint not available on this appliance")

func (c *Client) fetchHealthJSON() (json.RawMessage, error) {
	paths := []string{"/api/health", "/api/health/summary"}
	var lastErr error
	for _, p := range paths {
		body, err := c.GetRaw(p)
		if err != nil {
			lastErr = err
			continue
		}
		if json.Valid(body) {
			return json.RawMessage(body), nil
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errHealthNotAvailable
}

// LicenseSummary extracts display fields from license JSON when present.
func LicenseSummary(raw json.RawMessage) map[string]interface{} {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil
	}
	var root map[string]interface{}
	if json.Unmarshal(raw, &root) != nil {
		return nil
	}
	lic, _ := root["license"].(map[string]interface{})
	if lic == nil {
		lic = root
	}
	out := map[string]interface{}{}
	for _, k := range []string{"productName", "edition", "status", "expiration", "expiresAt", "maxHosts", "maxInstances", "maxCores", "maxMemory", "maxStorage"} {
		if v, ok := lic[k]; ok && v != nil && strings.TrimSpace(fmtString(v)) != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return map[string]interface{}{"raw": true}
	}
	return out
}

func fmtString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}
