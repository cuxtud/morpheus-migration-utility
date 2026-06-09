package migrate

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/cuxtud/morpheus-migration-utility/internal/morpheus"
)

type MigrateRequest struct {
	Source        ApplInfo       `json:"source"`
	Destination   ApplInfo       `json:"destination"`
	Items         []SelectedItem `json:"items"`
	DiscoveryID   int64          `json:"discoveryId,omitempty"`
	DiscoveryTime string         `json:"discoveryTime,omitempty"`
	// HttpDebug logs each Morpheus HTTP request (method, URL, JSON body) to the snapshot server stderr/log.
	// Does not appear in the browser; enable via migration UI checkbox or JSON httpDebug: true.
	HttpDebug bool `json:"httpDebug"`
	// ParallelCatalog controls concurrent catalog item migration after dependencies complete.
	// 0 = auto (up to 4 workers when multiple catalog items), 1 = sequential, N = worker count.
	ParallelCatalog int `json:"parallelCatalog,omitempty"`
}

type ApplInfo struct {
	ProfileID string `json:"profileId,omitempty"`
	URL       string `json:"url"`
	Token     string `json:"token"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	SkipTLS   bool   `json:"skipTls"`
}

// ClientFromApplInfo builds a Morpheus API client from migration appliance credentials.
func ClientFromApplInfo(a ApplInfo) (*morpheus.Client, error) {
	return clientFromApplInfo(a)
}

func clientFromApplInfo(a ApplInfo) (*morpheus.Client, error) {
	url := strings.TrimSpace(a.URL)
	if url == "" {
		return nil, fmt.Errorf("appliance url is required")
	}
	if strings.TrimSpace(a.Token) != "" {
		return morpheus.NewClient(url, a.Token, a.SkipTLS), nil
	}
	user := strings.TrimSpace(a.Username)
	if user != "" && a.Password != "" {
		return morpheus.NewClientFromPassword(url, user, a.Password, a.SkipTLS)
	}
	return nil, fmt.Errorf("api token or username and password required")
}

type SelectedItem struct {
	Category string `json:"category"`
	Type     string `json:"type"`
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	RawJSON  string `json:"rawJson"`
}

type MigrateResult struct {
	Results             []ItemResult `json:"results"`
	Success             int          `json:"success"`
	Created             int          `json:"created"`
	Updated             int          `json:"updated"`
	Failed              int          `json:"failed"`
	Blocked             int          `json:"blocked"`
	Partial             int          `json:"partial"`
	SourceDiscoveryID   int64        `json:"sourceDiscoveryId,omitempty"`
	SourceDiscoveryTime string       `json:"sourceDiscoveryTime,omitempty"`
}

type ItemResult struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Status string `json:"status"` // "success", "skipped", "error", "blocked", "partial"
	// Outcome classifies successful writes: "created" (new resource) or "updated" (synced existing).
	Outcome string `json:"outcome,omitempty"`
	Message string `json:"message"`
}

// categoryEndpoint maps item types to their create endpoints and payload wrapper keys
type endpointSpec struct {
	endpoint   string
	wrapperKey string
	// fields to strip before re-posting (server-managed fields)
	stripFields []string
}

var endpointMap = map[string]endpointSpec{
	"tenant":        {"/api/accounts", "account", []string{"id", "dateCreated", "lastUpdated", "stats"}},
	"role":          {"/api/roles", "role", []string{"id", "dateCreated", "lastUpdated", "owner"}},
	"group":         {"/api/groups", "group", []string{"id", "dateCreated", "lastUpdated", "stats", "zones"}},
	"policy":        {"/api/policies", "policy", []string{"id", "dateCreated", "lastUpdated"}},
	"task":          {"/api/tasks", "task", []string{"id", "dateCreated", "lastUpdated", "account", "accountId"}},
	"workflow":      {"/api/task-sets", "taskSet", []string{"id", "dateCreated", "lastUpdated", "account", "accountId"}},
	"layout":        {"/api/library/layouts", "layout", []string{"id", "dateCreated", "lastUpdated", "account", "accountId", "instanceTypeLayout"}},
	"nodeType":      {"/api/library/container-types", "containerType", []string{"id", "dateCreated", "lastUpdated", "account", "accountId"}},
	"blueprint":     {"/api/blueprints", "blueprint", []string{"id", "dateCreated", "lastUpdated", "account", "visibility"}},
	"credential":    {"/api/credentials", "credential", []string{"id", "dateCreated", "lastUpdated", "account"}},
	"storageBucket": {"/api/storage-buckets", "storageBucket", []string{"id", "dateCreated", "lastUpdated"}},
	"network":       {"/api/networks", "network", []string{"id", "dateCreated", "lastUpdated", "zone"}},
	"networkPool":   {"/api/networks/pools", "networkPool", []string{"id", "dateCreated", "lastUpdated"}},
	"networkDomain": {"/api/networks/domains", "networkDomain", []string{"id", "dateCreated", "lastUpdated"}},
	"virtualImage":  {"/api/virtual-images", "virtualImage", []string{"id", "dateCreated", "lastUpdated", "account", "storageProvider"}},
}

func Run(req MigrateRequest) *MigrateResult {
	return RunWithProgress(req, nil)
}

func RunWithProgress(req MigrateRequest, progress ProgressFunc) *MigrateResult {
	result := &MigrateResult{}
	emit := func(ev ProgressEvent) {
		if progress != nil {
			progress(ev)
		}
	}
	emit(ProgressEvent{Phase: "preparing", Message: "Connecting to destination appliance…"})

	dst, err := clientFromApplInfo(req.Destination)
	if err != nil {
		result := &MigrateResult{}
		result.Results = append(result.Results, ItemResult{
			Name: "destination", Type: "connection", Status: "error", Message: err.Error(),
		})
		result.Failed = 1
		return result
	}
	dst.HTTPDebug = req.HttpDebug

	var src *morpheus.Client
	if strings.TrimSpace(req.Source.URL) != "" {
		src, err = clientFromApplInfo(req.Source)
		if err != nil {
			result := &MigrateResult{}
			result.Results = append(result.Results, ItemResult{
				Name: "source", Type: "connection", Status: "error", Message: err.Error(),
			})
			result.Failed = 1
			return result
		}
		src.HTTPDebug = req.HttpDebug
		emit(ProgressEvent{Phase: "preparing", Message: "Connected to source and destination"})
	}

	state := newAutomationState()
	state.progress = progress
	items := req.Items
	if src != nil {
		emit(ProgressEvent{Phase: "preparing", Message: "Expanding migration dependencies…"})
		expanded, depErrs := expandMigrationDependencies(src, req.Items)
		items = expanded
		for _, msg := range depErrs {
			appendItemResult(result, ItemResult{
				Name:    "dependency-expansion",
				Type:    "dependency",
				Status:  "blocked",
				Message: msg,
			})
		}
	}
	items = filterItemsForInstanceTypeOrchestration(items)
	items = sortItemsForMigration(items)

	queue := make([]string, len(items))
	for i, it := range items {
		queue[i] = itemQueueLabel(it)
	}
	emit(ProgressEvent{
		Phase:   "queue",
		Message: fmt.Sprintf("Migration queue ready (%d items)", len(items)),
		Queue:   queue,
		Total:   len(items),
	})

	total := len(items)
	rec := newMigrationRecorder(result, emit)
	serialItems, catalogItems := partitionCatalogItems(items)
	workers := catalogParallelWorkers(req, len(catalogItems))

	for i, item := range serialItems {
		rec.runItem(src, dst, state, item, i+1, total)
	}

	if len(catalogItems) == 0 {
		return result
	}

	base := len(serialItems)
	if workers > 1 {
		rec.doneCount.Store(0)
		rec.runCatalogItemsParallel(src, dst, catalogItems, workers, total, base)
		return result
	}

	for i, item := range catalogItems {
		rec.runItem(src, dst, state, item, base+i+1, total)
	}

	return result
}

type itemKey struct {
	Type string
	ID   int64
}

func normalizeType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "instancetype":
		return "instanceType"
	case "layout":
		return "layout"
	case "nodetype", "containertype":
		return "nodeType"
	case "catalogitem":
		return "catalogItem"
	case "cloud", "zone":
		return "cloud"
	case "workflow", "taskset":
		return "workflow"
	default:
		return strings.TrimSpace(t)
	}
}

func keyForItem(it SelectedItem) itemKey {
	return itemKey{Type: normalizeType(it.Type), ID: it.ID}
}

func parseObject(raw string) map[string]interface{} {
	var m map[string]interface{}
	_ = json.Unmarshal([]byte(raw), &m)
	return m
}

func objectID(v interface{}) int64 {
	switch t := v.(type) {
	case map[string]interface{}:
		return intFromAny(t["id"])
	default:
		return 0
	}
}

func objectName(v interface{}) string {
	switch t := v.(type) {
	case map[string]interface{}:
		return stringFromAny(t["name"])
	default:
		return ""
	}
}

func extractLayoutDeps(layout map[string]interface{}) (nodeTypeIDs []int64, workflowIDs []int64) {
	addID := func(dst *[]int64, id int64) {
		if id <= 0 {
			return
		}
		for _, v := range *dst {
			if v == id {
				return
			}
		}
		*dst = append(*dst, id)
	}

	// Most layouts reference node types via containerTypes.
	if arr, ok := layout["containerTypes"].([]interface{}); ok {
		for _, e := range arr {
			addID(&nodeTypeIDs, objectID(e))
		}
	}
	// Some payloads use nodeTypes directly.
	if arr, ok := layout["nodeTypes"].([]interface{}); ok {
		for _, e := range arr {
			addID(&nodeTypeIDs, objectID(e))
		}
	}
	// Some include layout containers with nested node/container type.
	if arr, ok := layout["layoutContainers"].([]interface{}); ok {
		for _, e := range arr {
			m, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			addID(&nodeTypeIDs, objectID(m["containerType"]))
			addID(&nodeTypeIDs, objectID(m["nodeType"]))
		}
	}

	// Workflow references appear under taskSet/workflow/provisionWorkflow in different payloads.
	if arr, ok := layout["taskSets"].([]interface{}); ok {
		for _, e := range arr {
			addID(&workflowIDs, objectID(e))
		}
	}
	addID(&workflowIDs, objectID(layout["taskSet"]))
	addID(&workflowIDs, objectID(layout["workflow"]))
	addID(&workflowIDs, objectID(layout["provisionWorkflow"]))
	return nodeTypeIDs, workflowIDs
}

func extractInstanceTypeLayoutIDs(obj map[string]interface{}) []int64 {
	var out []int64
	add := func(id int64) {
		if id <= 0 {
			return
		}
		for _, v := range out {
			if v == id {
				return
			}
		}
		out = append(out, id)
	}
	if arr, ok := obj["instanceTypeLayouts"].([]interface{}); ok {
		for _, e := range arr {
			add(objectID(e))
		}
	}
	if arr, ok := obj["layouts"].([]interface{}); ok {
		for _, e := range arr {
			add(objectID(e))
		}
	}
	return out
}

// FetchFullNodeType loads a complete container type record from the source appliance.
func FetchFullNodeType(src *morpheus.Client, id int64) (map[string]interface{}, error) {
	item, err := fetchSourceByID(src, "nodeType", id)
	if err != nil {
		return nil, err
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(item.RawJSON), &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func fetchSourceByID(src *morpheus.Client, typ string, id int64) (SelectedItem, error) {
	switch normalizeType(typ) {
	case "layout":
		return fetchSourceResource(src,
			fmt.Sprintf("/api/library/layouts/%d", id),
			"Layouts", "layout",
			[]string{"instanceTypeLayout", "layout"},
		)
	case "nodeType":
		return fetchSourceResource(src,
			fmt.Sprintf("/api/library/container-types/%d", id),
			"Node Types", "nodeType",
			[]string{"containerType"},
		)
	case "workflow":
		return fetchSourceResource(src,
			fmt.Sprintf("/api/task-sets/%d", id),
			"Workflows", "workflow",
			[]string{"taskSet"},
		)
	default:
		return SelectedItem{}, fmt.Errorf("unsupported dependency type %q", typ)
	}
}

func fetchSourceResource(src *morpheus.Client, path, category, typ string, wrapperKeys []string) (SelectedItem, error) {
	body, err := src.GetRaw(path)
	if err != nil {
		return SelectedItem{}, err
	}
	raw, key, err := unwrapFirstJSONKey(body, wrapperKeys)
	if err != nil {
		return SelectedItem{}, err
	}
	_ = key
	obj := map[string]interface{}{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return SelectedItem{}, err
	}
	name := stringFromAny(obj["name"])
	if name == "" {
		name = stringFromAny(obj["code"])
	}
	return SelectedItem{
		Category: category,
		Type:     normalizeType(typ),
		ID:       intFromAny(obj["id"]),
		Name:     name,
		RawJSON:  string(raw),
	}, nil
}

func unwrapFirstJSONKey(body []byte, keys []string) (json.RawMessage, string, error) {
	var wrap map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, "", err
	}
	for _, key := range keys {
		if raw, ok := wrap[key]; ok {
			return raw, key, nil
		}
	}
	return nil, "", fmt.Errorf("source response missing %q", strings.Join(keys, `" or "`))
}

func expandMigrationDependencies(src *morpheus.Client, in []SelectedItem) ([]SelectedItem, []string) {
	items := make([]SelectedItem, len(in))
	copy(items, in)

	byKey := map[itemKey]SelectedItem{}
	for _, it := range items {
		it.Type = normalizeType(it.Type)
		byKey[keyForItem(it)] = it
	}

	var errs []string
	changed := true
	for changed {
		changed = false
		keys := make([]itemKey, 0, len(byKey))
		for k := range byKey {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].Type == keys[j].Type {
				return keys[i].ID < keys[j].ID
			}
			return keys[i].Type < keys[j].Type
		})

		for _, k := range keys {
			it := byKey[k]
			obj := parseObject(it.RawJSON)
			if obj == nil {
				continue
			}
			switch it.Type {
			case "instanceType":
				for _, lid := range extractInstanceTypeLayoutIDs(obj) {
					lk := itemKey{Type: "layout", ID: lid}
					if _, ok := byKey[lk]; ok {
						continue
					}
					dep, err := fetchSourceByID(src, "layout", lid)
					if err != nil {
						errs = append(errs, fmt.Sprintf("instance type %q: required layout %d not retrievable (%v)", it.Name, lid, err))
						continue
					}
					byKey[lk] = dep
					changed = true
				}
			case "layout":
				nodeIDs, wfIDs := extractLayoutDeps(obj)
				for _, nid := range nodeIDs {
					nk := itemKey{Type: "nodeType", ID: nid}
					if _, ok := byKey[nk]; ok {
						continue
					}
					dep, err := fetchSourceByID(src, "nodeType", nid)
					if err != nil {
						errs = append(errs, fmt.Sprintf("layout %q: required node type %d not retrievable (%v)", it.Name, nid, err))
						continue
					}
					byKey[nk] = dep
					changed = true
				}
				for _, wid := range wfIDs {
					wk := itemKey{Type: "workflow", ID: wid}
					if _, ok := byKey[wk]; ok {
						continue
					}
					dep, err := fetchSourceByID(src, "workflow", wid)
					if err != nil {
						errs = append(errs, fmt.Sprintf("layout %q: required workflow %d not retrievable (%v)", it.Name, wid, err))
						continue
					}
					byKey[wk] = dep
					changed = true
				}
			case "catalogItem":
				obj := parseObject(it.RawJSON)
				if extractCatalogInstanceTypeCode(obj) == "" && it.ID > 0 {
					if full, err := fetchFullCatalogItem(src, it.ID); err == nil {
						obj = full
						it.RawJSON = mustJSON(full)
						byKey[k] = it
					}
				}
				itCode := extractCatalogInstanceTypeCode(obj)
				if itCode == "" {
					continue
				}
				srcIT, err := findSourceInstanceTypeByCode(src, itCode)
				if err != nil {
					errs = append(errs, fmt.Sprintf("catalog item %q: required instance type %q not retrievable (%v)", it.Name, itCode, err))
					continue
				}
				ik := itemKey{Type: "instanceType", ID: srcIT.ID}
				if _, ok := byKey[ik]; ok {
					continue
				}
				byKey[ik] = srcIT
				changed = true
			}
		}
	}

	out := make([]SelectedItem, 0, len(byKey))
	for _, it := range byKey {
		out = append(out, it)
	}
	return out, errs
}

func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func appendItemResult(result *MigrateResult, r ItemResult) {
	result.Results = append(result.Results, r)
	switch r.Status {
	case "success":
		result.Success++
		switch strings.ToLower(strings.TrimSpace(r.Outcome)) {
		case "updated":
			result.Updated++
		default:
			// "created" or unset — generic migrations count as created
			result.Created++
		}
	case "skipped":
		// not counted as failed
	case "blocked":
		result.Blocked++
	case "partial":
		result.Partial++
	case "error":
		result.Failed++
	default:
		if r.Status != "" {
			result.Failed++
		}
	}
}

func buildPayload(rawJSON string, spec endpointSpec) ([]byte, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &obj); err != nil {
		return nil, err
	}

	// Strip server-managed fields
	for _, f := range spec.stripFields {
		delete(obj, f)
	}

	wrapper := map[string]interface{}{
		spec.wrapperKey: obj,
	}
	return json.Marshal(wrapper)
}

func buildNodeTypePayloadWithVirtualImage(dst *morpheus.Client, rawJSON string, spec endpointSpec) ([]byte, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(rawJSON), &obj); err != nil {
		return nil, err
	}
	for _, f := range spec.stripFields {
		delete(obj, f)
	}

	if err := remapNodeTypeVirtualImage(dst, obj); err != nil {
		return nil, err
	}

	return json.Marshal(map[string]interface{}{spec.wrapperKey: obj})
}

func remapNodeTypeVirtualImage(dst *morpheus.Client, nodeType map[string]interface{}) error {
	vi, ok := nodeType["virtualImage"].(map[string]interface{})
	if !ok || vi == nil {
		return nil // node type can be image-less
	}
	srcName := strings.TrimSpace(stringFromAny(vi["name"]))
	if srcName == "" {
		return fmt.Errorf("node type references virtual image without a name")
	}

	dstID, err := findDestinationVirtualImageIDByName(dst, srcName)
	if err != nil {
		return err
	}
	if dstID <= 0 {
		return fmt.Errorf("related virtual image %q not found on destination", srcName)
	}
	nodeType["virtualImage"] = map[string]interface{}{"id": dstID, "name": srcName}
	return nil
}

func findDestinationVirtualImageIDByName(dst *morpheus.Client, sourceName string) (int64, error) {
	name := strings.TrimSpace(sourceName)
	if name == "" {
		return 0, nil
	}
	escaped := url.QueryEscape(name)
	queries := []string{
		fmt.Sprintf("/api/virtual-images?max=100&offset=0&name=%s&filterType=All", escaped),
		fmt.Sprintf("/api/virtual-images?phrase=%s&max=100&offset=0", escaped),
	}
	for _, path := range queries {
		id, err := virtualImageIDFromListQuery(dst, path, name)
		if err != nil {
			return 0, err
		}
		if id > 0 {
			return id, nil
		}
	}
	return 0, nil
}

func virtualImageIDFromListQuery(dst *morpheus.Client, path, wantName string) (int64, error) {
	body, err := dst.GetRaw(path)
	if err != nil {
		return 0, fmt.Errorf("query virtual images: %v", err)
	}
	var wrap map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrap); err != nil {
		return 0, err
	}
	raw, ok := wrap["virtualImages"]
	if !ok {
		return 0, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return 0, err
	}
	id := virtualImageIDFromItems(items, wantName)
	if id > 0 {
		return id, nil
	}
	return 0, nil
}

func virtualImageIDFromItems(items []json.RawMessage, wantName string) int64 {
	want := strings.ToLower(strings.TrimSpace(wantName))
	for _, it := range items {
		var row map[string]interface{}
		if json.Unmarshal(it, &row) != nil {
			continue
		}
		n := strings.ToLower(strings.TrimSpace(stringFromAny(row["name"])))
		if n == want {
			if id := intFromAny(row["id"]); id > 0 {
				return id
			}
		}
	}
	return 0
}
