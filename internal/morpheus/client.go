package morpheus

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultHTTPTimeout = 3 * time.Minute

type Client struct {
	BaseURL    string
	Token      string
	httpClient *http.Client
	// HTTPDebug logs outgoing requests when true. Also enabled by env MORPHEUS_SNAPSHOT_HTTP_DEBUG=1
	// on the process running this client (e.g. the snapshot server).
	HTTPDebug bool
}

func httpDebugEnvEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("MORPHEUS_SNAPSHOT_HTTP_DEBUG")))
	return v == "1" || v == "true" || v == "yes"
}

func (c *Client) httpDebugEnabled() bool {
	return c != nil && (c.HTTPDebug || httpDebugEnvEnabled())
}

// logHTTPDebug writes method, full URL, and body to stderr and the standard log (server terminal).
func (c *Client) logHTTPDebug(method, path string, payload []byte) {
	if !c.httpDebugEnabled() {
		return
	}
	u := strings.TrimRight(c.BaseURL, "/") + path
	body := "(no body)"
	if len(payload) > 0 {
		body = string(payload)
		const max = 256 * 1024
		if len(body) > max {
			body = body[:max] + fmt.Sprintf("\n... [truncated, %d bytes total]", len(payload))
		}
	}
	msg := fmt.Sprintf("[morpheus-snapshot http debug] %s %s\n%s\n", method, u, body)
	log.Print(msg)
	fmt.Fprint(os.Stderr, msg)
}

func httpClientTimeout() time.Duration {
	v := strings.TrimSpace(os.Getenv("MORPHEUS_SNAPSHOT_HTTP_TIMEOUT"))
	if v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return defaultHTTPTimeout
}

func newHTTPTransport(skipTLS bool) *http.Transport {
	return &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: skipTLS},
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 32,
		IdleConnTimeout:     90 * time.Second,
	}
}

func NewClient(baseURL, token string, skipTLS bool) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		httpClient: &http.Client{
			Timeout:   httpClientTimeout(),
			Transport: newHTTPTransport(skipTLS),
		},
	}
}

func isRetryableGetErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "broken pipe") ||
		strings.Contains(s, "eof")
}

func (c *Client) getOnce(path string) ([]byte, error) {
	c.logHTTPDebug(http.MethodGet, path, nil)
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

func (c *Client) get(path string) ([]byte, error) {
	const attempts = 2
	var last error
	for i := 0; i < attempts; i++ {
		body, err := c.getOnce(path)
		if err == nil {
			return body, nil
		}
		last = err
		if i == attempts-1 || !isRetryableGetErr(err) {
			return nil, err
		}
		time.Sleep(time.Duration(i+1) * time.Second)
	}
	return nil, last
}

func (c *Client) post(path string, payload []byte) ([]byte, error) {
	c.logHTTPDebug(http.MethodPost, path, payload)
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

func (c *Client) put(path string, payload []byte) ([]byte, error) {
	c.logHTTPDebug(http.MethodPut, path, payload)
	req, err := http.NewRequest(http.MethodPut, c.BaseURL+path, strings.NewReader(string(payload)))
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

// GetRaw performs a GET request and returns the response body.
func (c *Client) GetRaw(path string) ([]byte, error) {
	return c.get(path)
}

// PutRaw performs a PUT request with a JSON body.
func (c *Client) PutRaw(path string, payload []byte) ([]byte, error) {
	return c.put(path, payload)
}

func (c *Client) delete(path string) error {
	req, err := http.NewRequest(http.MethodDelete, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// DeleteRaw sends an HTTP DELETE to path.
func (c *Client) DeleteRaw(path string) error {
	return c.delete(path)
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
	Categories   []CategoryGroup `json:"categories"`
	Total        int             `json:"total"`
	Errors       []string        `json:"errors"`
	DiscoveredAt string          `json:"discoveredAt,omitempty"`
	DurationMs   int64           `json:"durationMs,omitempty"`
}

type CategoryGroup struct {
	Name       string          `json:"name"`
	Icon       string          `json:"icon"`
	ParentPath string          `json:"parentPath,omitempty"` // e.g. "Library/Automation"
	Items      []DiscoveryItem `json:"items"`
}

// paginate fetches all pages of a list endpoint
func (c *Client) paginate(basePath string, dataKey string) ([]json.RawMessage, error) {
	return c.paginateWithPageSize(basePath, dataKey, 50)
}

func (c *Client) paginateWithPageSize(basePath, dataKey string, pageSize int) ([]json.RawMessage, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	var all []json.RawMessage
	offset := 0
	max := pageSize
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

func extractBoolField(raw json.RawMessage, field string) (bool, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false, false
	}
	val, ok := obj[field]
	if !ok {
		return false, false
	}
	var b bool
	if err := json.Unmarshal(val, &b); err == nil {
		return b, true
	}
	var s string
	if err := json.Unmarshal(val, &s); err == nil {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true", "yes", "1":
			return true, true
		case "false", "no", "0":
			return false, true
		}
	}
	return false, false
}

func extractNestedStringField(raw json.RawMessage, field string, nested string) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	val, ok := obj[field]
	if !ok {
		return ""
	}
	return extractStringField(val, nested)
}

func hasNonNullObjectField(raw json.RawMessage, field string) bool {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false
	}
	val, ok := obj[field]
	if !ok {
		return false
	}
	v := strings.TrimSpace(string(val))
	if v == "" || v == "null" {
		return false
	}
	var nested map[string]interface{}
	return json.Unmarshal(val, &nested) == nil && len(nested) > 0
}

// IsSeededSystemLibraryItem checks common Morpheus flags used by built-in seeded library records.
// We only use this for migration-sensitive categories (instance types, layouts, node types).
func IsSeededSystemLibraryItem(raw json.RawMessage) bool {
	// Strong signal from Morpheus library payloads:
	// seeded/system records typically have account: null
	// user-created records have account object (e.g. Master tenant).
	if !hasNonNullObjectField(raw, "account") {
		return true
	}

	// Explicit booleans used by many Morpheus objects.
	for _, key := range []string{"isSystem", "system", "seeded", "builtIn", "internal"} {
		if v, ok := extractBoolField(raw, key); ok && v {
			return true
		}
	}

	// Some APIs expose a source identifier for seeded content.
	for _, key := range []string{"source", "sourceType", "provisionTypeCode", "code"} {
		v := strings.ToLower(strings.TrimSpace(extractStringField(raw, key)))
		if strings.Contains(v, "morpheus") || strings.Contains(v, "system") || strings.Contains(v, "seed") {
			return true
		}
	}

	// Some payloads expose nested owner/provider metadata.
	for _, p := range [][2]string{{"owner", "code"}, {"owner", "name"}, {"provider", "code"}, {"provider", "name"}} {
		v := strings.ToLower(strings.TrimSpace(extractNestedStringField(raw, p[0], p[1])))
		if strings.Contains(v, "morpheus") || strings.Contains(v, "system") || strings.Contains(v, "seed") {
			return true
		}
	}

	return false
}

func (c *Client) discoverCategoryItems(f discoveryFetcher, dc *discoveryConcurrency) ([]DiscoveryItem, string) {
	rawItems, err := c.paginateDiscoveryList(f.endpoint, f.dataKey, discoveryListPageSize)
	if err != nil {
		return nil, fmt.Sprintf("%s: %v", f.category, err)
	}
	enriched := c.parallelEnrichDiscoveryItems(rawItems, f.dataKey, dc)

	seen := map[int64]struct{}{}
	items := make([]DiscoveryItem, 0, len(enriched))
	for _, itemRaw := range enriched {
		id := extractInt64Field(itemRaw, "id")

		if f.typeHint == "instanceType" || f.typeHint == "layout" || f.typeHint == "nodeType" {
			if IsSeededSystemLibraryItem(itemRaw) {
				continue
			}
		}

		if id > 0 {
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
		}

		name := discoveryItemName(itemRaw, f.typeHint)
		desc := extractStringField(itemRaw, "description")
		subType := discoverySubType(itemRaw, f.subField)

		items = append(items, DiscoveryItem{
			ID:          id,
			Name:        name,
			Type:        f.typeHint,
			Description: desc,
			Category:    f.category,
			SubType:     subType,
			RawJSON:     string(itemRaw),
		})
	}
	return items, ""
}

func discoverySubType(itemRaw json.RawMessage, subField string) string {
	if subField == "" {
		return ""
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(itemRaw, &obj) != nil {
		return ""
	}
	nested, ok := obj[subField]
	if !ok {
		return ""
	}
	subType := extractStringField(nested, "name")
	if subType == "" {
		_ = json.Unmarshal(nested, &subType)
	}
	return subType
}

func (c *Client) parallelEnrichDiscoveryItems(rawItems []json.RawMessage, dataKey string, dc *discoveryConcurrency) []json.RawMessage {
	if !discoveryItemNeedsEnrich(dataKey) || len(rawItems) == 0 {
		return rawItems
	}
	out := make([]json.RawMessage, len(rawItems))
	sem := make(chan struct{}, discoveryEnrichParallelism)
	var wg sync.WaitGroup
	for i, raw := range rawItems {
		wg.Add(1)
		go func(i int, raw json.RawMessage) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if dc != nil && dc.enrichSem != nil {
				dc.enrichSem <- struct{}{}
				defer func() { <-dc.enrichSem }()
			}
			id := extractInt64Field(raw, "id")
			out[i] = c.enrichDiscoveryItemRaw(raw, dataKey, id)
		}(i, raw)
	}
	wg.Wait()
	return out
}

func discoveryItemNeedsEnrich(dataKey string) bool {
	switch dataKey {
	case "taskSets", "catalogItemTypes", "containerTypes", "instanceTypes", "optionTypeForms":
		return true
	default:
		return false
	}
}

func (c *Client) enrichDiscoveryItemRaw(itemRaw json.RawMessage, dataKey string, id int64) json.RawMessage {
	if id <= 0 {
		return itemRaw
	}
	switch dataKey {
	case "taskSets":
		if body, err := c.get(fmt.Sprintf("/api/task-sets/%d", id)); err == nil {
			var w map[string]json.RawMessage
			if json.Unmarshal(body, &w) == nil {
				if ts, ok := w["taskSet"]; ok {
					return ts
				}
			}
		}
	case "catalogItemTypes":
		if body, err := c.get(fmt.Sprintf("/api/catalog-item-types/%d", id)); err == nil {
			var w map[string]json.RawMessage
			if json.Unmarshal(body, &w) == nil {
				if cit, ok := w["catalogItemType"]; ok {
					return cit
				}
			}
		}
	case "containerTypes":
		if body, err := c.get(fmt.Sprintf("/api/library/container-types/%d", id)); err == nil {
			var w map[string]json.RawMessage
			if json.Unmarshal(body, &w) == nil {
				if ct, ok := w["containerType"]; ok {
					return ct
				}
			}
		}
	case "instanceTypes":
		for _, path := range []string{
			fmt.Sprintf("/api/library/instance-types/%d", id),
			fmt.Sprintf("/api/instance-types/%d", id),
		} {
			if body, err := c.get(path); err == nil {
				var w map[string]json.RawMessage
				if json.Unmarshal(body, &w) == nil {
					if it, ok := w["instanceType"]; ok {
						return it
					}
				}
			}
		}
	case "optionTypeForms":
		if body, err := c.get(fmt.Sprintf("/api/library/option-type-forms/%d", id)); err == nil {
			var w map[string]json.RawMessage
			if json.Unmarshal(body, &w) == nil {
				if tf, ok := w["optionTypeForm"]; ok {
					return tf
				}
			}
		}
	}
	return itemRaw
}

type discoveryFetcher struct {
	category   string
	icon       string
	endpoint   string
	dataKey    string
	typeHint   string
	subField   string
	parentPath string
}

func (c *Client) Discover() *DiscoveryResult {
	started := time.Now()
	result := &DiscoveryResult{
		DiscoveredAt: started.UTC().Format(time.RFC3339),
	}
	defer func() {
		result.DurationMs = time.Since(started).Milliseconds()
	}()

	fetchers := []discoveryFetcher{
		// Infrastructure / Clouds (Morpheus API uses /api/zones; /api/clouds is not on all versions)
		{category: "Clouds", icon: "cloud", endpoint: "/api/zones", dataKey: "zones", typeHint: "cloud", subField: "zoneType"},

		// Integrations
		{category: "Integrations", icon: "plug", endpoint: "/api/integrations", dataKey: "integrations", typeHint: "integration", subField: "integrationType"},

		// Compute / library
		{category: "Instance Types", icon: "template", endpoint: "/api/library/instance-types", dataKey: "instanceTypes", typeHint: "instanceType", parentPath: "Library"},
		{category: "Layouts", icon: "template", endpoint: "/api/library/layouts", dataKey: "layouts", typeHint: "layout", parentPath: "Library"},
		{category: "Node Types", icon: "template", endpoint: "/api/library/container-types", dataKey: "containerTypes", typeHint: "nodeType", parentPath: "Library"},
		{category: "Virtual Images", icon: "template", endpoint: "/api/virtual-images", dataKey: "virtualImages", typeHint: "virtualImage", parentPath: "Library"},

		// Catalog & Blueprints
		{category: "Catalog Items", icon: "catalog", endpoint: "/api/catalog-item-types", dataKey: "catalogItemTypes", typeHint: "catalogItem"},
		{category: "Blueprints", icon: "blueprint", endpoint: "/api/blueprints", dataKey: "blueprints", typeHint: "blueprint"},
		{category: "Apps", icon: "app", endpoint: "/api/apps", dataKey: "apps", typeHint: "app"},

		// Automation
		{category: "Tasks", icon: "task", endpoint: "/api/tasks", dataKey: "tasks", typeHint: "task", subField: "taskType", parentPath: "Library/Automation"},
		{category: "Workflows", icon: "workflow", endpoint: "/api/task-sets", dataKey: "taskSets", typeHint: "workflow", parentPath: "Library/Automation"},
		{category: "Inputs", icon: "form", endpoint: "/api/library/option-types", dataKey: "optionTypes", typeHint: "input", subField: "type", parentPath: "Library/Options"},
		{category: "Option Lists", icon: "list", endpoint: "/api/library/option-type-lists", dataKey: "optionTypeLists", typeHint: "optionList", subField: "type", parentPath: "Library/Options"},
		{category: "Forms", icon: "form", endpoint: "/api/library/option-type-forms", dataKey: "optionTypeForms", typeHint: "form", parentPath: "Library/Options"},

		// Policies & RBAC
		{category: "Tenants", icon: "tenant", endpoint: "/api/accounts", dataKey: "accounts", typeHint: "tenant"},
		{category: "Roles", icon: "role", endpoint: "/api/roles", dataKey: "roles", typeHint: "role"},
		{category: "Users", icon: "user", endpoint: "/api/users", dataKey: "users", typeHint: "user"},
		{category: "Policies", icon: "policy", endpoint: "/api/policies", dataKey: "policies", typeHint: "policy"},
		{category: "Groups", icon: "group", endpoint: "/api/groups", dataKey: "groups", typeHint: "group"},

		// Cypher
		{category: "Cypher", icon: "lock", endpoint: "/api/cypher", dataKey: "cyphers", typeHint: "cypher"},
	}

	type categoryWork struct {
		f     discoveryFetcher
		items []DiscoveryItem
		err   string
	}
	dc := newDiscoveryConcurrency()
	workCh := make(chan categoryWork, len(fetchers))
	var listWG sync.WaitGroup
	for _, f := range fetchers {
		listWG.Add(1)
		go func(f discoveryFetcher) {
			defer listWG.Done()
			items, errMsg := c.discoverCategoryItems(f, dc)
			workCh <- categoryWork{f: f, items: items, err: errMsg}
		}(f)
	}
	listWG.Wait()
	close(workCh)

	categoryMap := map[string]*CategoryGroup{}
	for cw := range workCh {
		if cw.err != "" {
			result.Errors = append(result.Errors, cw.err)
			continue
		}
		if len(cw.items) == 0 {
			continue
		}
		grp, exists := categoryMap[cw.f.category]
		if !exists {
			grp = &CategoryGroup{Name: cw.f.category, Icon: cw.f.icon, ParentPath: cw.f.parentPath}
			categoryMap[cw.f.category] = grp
		}
		grp.Items = append(grp.Items, cw.items...)
		result.Total += len(cw.items)
	}

	var categoryNames []string
	for name := range categoryMap {
		categoryNames = append(categoryNames, name)
	}
	sort.Slice(categoryNames, func(i, j int) bool {
		a := categoryMap[categoryNames[i]]
		b := categoryMap[categoryNames[j]]
		ap := strings.ToLower(a.ParentPath + "/" + a.Name)
		bp := strings.ToLower(b.ParentPath + "/" + b.Name)
		return ap < bp
	})
	for _, name := range categoryNames {
		grp := categoryMap[name]
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
