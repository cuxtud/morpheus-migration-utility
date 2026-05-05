package morpheus

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string
	Token      string
	httpClient *http.Client
}

func NewClient(baseURL, token string, skipTLS bool) *Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: skipTLS},
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

func (c *Client) get(path string) ([]byte, error) {
	req, err := http.NewRequest("GET", c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func (c *Client) post(path string, payload []byte) ([]byte, error) {
	req, err := http.NewRequest("POST", c.BaseURL+path, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// DiscoveryItem is a generic discovered resource
type DiscoveryItem struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category"`
	SubType     string `json:"subType,omitempty"`
	RawJSON     string `json:"rawJson,omitempty"`
}

// DiscoveryResult holds all discovered items grouped by category
type DiscoveryResult struct {
	Categories []CategoryGroup `json:"categories"`
	Total      int             `json:"total"`
	Errors     []string        `json:"errors"`
}

type CategoryGroup struct {
	Name  string          `json:"name"`
	Icon  string          `json:"icon"`
	Items []DiscoveryItem `json:"items"`
}

// paginate fetches all pages of a list endpoint
func (c *Client) paginate(basePath string, dataKey string) ([]json.RawMessage, error) {
	var all []json.RawMessage
	offset := 0
	max := 50
	for {
		path := fmt.Sprintf("%s?max=%d&offset=%d", basePath, max, offset)
		body, err := c.get(path)
		if err != nil {
			return nil, err
		}
		var wrapper map[string]json.RawMessage
		if err := json.Unmarshal(body, &wrapper); err != nil {
			return nil, err
		}
		raw, ok := wrapper[dataKey]
		if !ok {
			break
		}
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			break
		}
		all = append(all, items...)
		if len(items) < max {
			break
		}
		offset += max
	}
	return all, nil
}

func extractStringField(raw json.RawMessage, field string) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	val, ok := obj[field]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(val, &s); err != nil {
		// might be a number
		return strings.Trim(string(val), `"`)
	}
	return s
}

func extractInt64Field(raw json.RawMessage, field string) int64 {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return 0
	}
	val, ok := obj[field]
	if !ok {
		return 0
	}
	var n int64
	if err := json.Unmarshal(val, &n); err != nil {
		return 0
	}
	return n
}

func (c *Client) Discover() *DiscoveryResult {
	result := &DiscoveryResult{}

	type fetcher struct {
		category string
		icon     string
		endpoint string
		dataKey  string
		typeHint string
		subField string // optional nested field for sub-type
	}

	fetchers := []fetcher{
		// Infrastructure / Clouds
		{category: "Clouds", icon: "cloud", endpoint: "/api/zones", dataKey: "zones", typeHint: "cloud", subField: "zoneType"},
		{category: "Clouds", icon: "cloud", endpoint: "/api/clouds", dataKey: "zones", typeHint: "cloud", subField: "zoneType"},

		// Integrations
		{category: "Integrations", icon: "plug", endpoint: "/api/integrations", dataKey: "integrations", typeHint: "integration", subField: "integrationType"},

		// Network
		{category: "Networks", icon: "network", endpoint: "/api/networks", dataKey: "networks", typeHint: "network"},
		{category: "Network Pools", icon: "network", endpoint: "/api/networks/pools", dataKey: "networkPools", typeHint: "networkPool"},
		{category: "Network Domains", icon: "network", endpoint: "/api/networks/domains", dataKey: "networkDomains", typeHint: "networkDomain"},

		// Compute
		{category: "Instances", icon: "server", endpoint: "/api/instances", dataKey: "instances", typeHint: "instance", subField: "instanceType"},
		{category: "Virtual Images", icon: "image", endpoint: "/api/virtual-images", dataKey: "virtualImages", typeHint: "virtualImage"},
		{category: "Instance Types", icon: "template", endpoint: "/api/library/instance-types", dataKey: "instanceTypes", typeHint: "instanceType"},
		{category: "Layouts", icon: "template", endpoint: "/api/library/layouts", dataKey: "layouts", typeHint: "layout"},
		{category: "Node Types", icon: "template", endpoint: "/api/library/container-types", dataKey: "containerTypes", typeHint: "nodeType"},

		// Catalog & Blueprints
		{category: "Catalog Items", icon: "catalog", endpoint: "/api/catalog-item-types", dataKey: "catalogItemTypes", typeHint: "catalogItem"},
		{category: "Blueprints", icon: "blueprint", endpoint: "/api/blueprints", dataKey: "blueprints", typeHint: "blueprint"},
		{category: "Apps", icon: "app", endpoint: "/api/apps", dataKey: "apps", typeHint: "app"},

		// Automation
		{category: "Tasks", icon: "task", endpoint: "/api/tasks", dataKey: "tasks", typeHint: "task", subField: "taskType"},
		{category: "Workflows", icon: "workflow", endpoint: "/api/task-sets", dataKey: "taskSets", typeHint: "workflow"},
		{category: "Executions", icon: "run", endpoint: "/api/execution-request", dataKey: "executionRequests", typeHint: "execution"},

		// Policies & RBAC
		{category: "Tenants", icon: "tenant", endpoint: "/api/accounts", dataKey: "accounts", typeHint: "tenant"},
		{category: "Roles", icon: "role", endpoint: "/api/roles", dataKey: "roles", typeHint: "role"},
		{category: "Users", icon: "user", endpoint: "/api/users", dataKey: "users", typeHint: "user"},
		{category: "Policies", icon: "policy", endpoint: "/api/policies", dataKey: "policies", typeHint: "policy"},
		{category: "Groups", icon: "group", endpoint: "/api/groups", dataKey: "groups", typeHint: "group"},

		// Credentials & Security
		{category: "Credentials", icon: "key", endpoint: "/api/credentials", dataKey: "credentials", typeHint: "credential", subField: "type"},

		// Storage
		{category: "Storage Buckets", icon: "storage", endpoint: "/api/storage-buckets", dataKey: "storageBuckets", typeHint: "storageBucket"},
		{category: "Storage Servers", icon: "storage", endpoint: "/api/storage-servers", dataKey: "storageServers", typeHint: "storageServer"},

		// Monitoring
		{category: "Monitors", icon: "monitor", endpoint: "/api/monitoring/checks", dataKey: "checks", typeHint: "monitorCheck"},
		{category: "Alerts", icon: "alert", endpoint: "/api/monitoring/alerts", dataKey: "alerts", typeHint: "alert"},

		// Kubernetes
		{category: "Clusters", icon: "cluster", endpoint: "/api/clusters", dataKey: "clusters", typeHint: "cluster", subField: "type"},

		// Cypher
		{category: "Cypher", icon: "lock", endpoint: "/api/cypher", dataKey: "cyphers", typeHint: "cypher"},
	}

	// Deduplicate categories
	categoryMap := map[string]*CategoryGroup{}

	for _, f := range fetchers {
		items, err := c.paginate(f.endpoint, f.dataKey)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", f.category, err))
			continue
		}

		grp, exists := categoryMap[f.category]
		if !exists {
			grp = &CategoryGroup{Name: f.category, Icon: f.icon}
			categoryMap[f.category] = grp
		}

		for _, raw := range items {
			id := extractInt64Field(raw, "id")
			name := extractStringField(raw, "name")
			if name == "" {
				name = extractStringField(raw, "username")
			}
			desc := extractStringField(raw, "description")
			subType := ""
			if f.subField != "" {
				// subField might be nested object with "name"
				var obj map[string]json.RawMessage
				json.Unmarshal(raw, &obj)
				if nested, ok := obj[f.subField]; ok {
					subType = extractStringField(nested, "name")
					if subType == "" {
						json.Unmarshal(nested, &subType)
					}
				}
			}

			grp.Items = append(grp.Items, DiscoveryItem{
				ID:          id,
				Name:        name,
				Type:        f.typeHint,
				Description: desc,
				Category:    f.category,
				SubType:     subType,
				RawJSON:     string(raw),
			})
			result.Total++
		}
	}

	for _, grp := range categoryMap {
		if len(grp.Items) > 0 {
			result.Categories = append(result.Categories, *grp)
		}
	}
	return result
}

// PostRaw posts raw JSON payload to a path
func (c *Client) PostRaw(path string, payload []byte) ([]byte, error) {
	return c.post(path, payload)
}

// TestConnection verifies the token works
func (c *Client) TestConnection() (string, error) {
	body, err := c.get("/api/whoami")
	if err != nil {
		return "", err
	}
	var resp struct {
		User struct {
			Username    string `json:"username"`
			DisplayName string `json:"displayName"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}
	name := resp.User.DisplayName
	if name == "" {
		name = resp.User.Username
	}
	return name, nil
}
