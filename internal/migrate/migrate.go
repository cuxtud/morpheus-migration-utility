package migrate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anish/morpheus-snapshot/internal/morpheus"
)

type MigrateRequest struct {
	Source      ApplInfo              `json:"source"`
	Destination ApplInfo              `json:"destination"`
	Items       []SelectedItem        `json:"items"`
	// HttpDebug logs each Morpheus HTTP request (method, URL, JSON body) to the snapshot server stderr/log.
	// Does not appear in the browser; enable via migration UI checkbox or JSON httpDebug: true.
	HttpDebug bool `json:"httpDebug"`
}

type ApplInfo struct {
	URL     string `json:"url"`
	Token   string `json:"token"`
	SkipTLS bool   `json:"skipTls"`
}

type SelectedItem struct {
	Category string `json:"category"`
	Type     string `json:"type"`
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	RawJSON  string `json:"rawJson"`
}

type MigrateResult struct {
	Results []ItemResult `json:"results"`
	Success int          `json:"success"`
	Created int          `json:"created"`
	Updated int          `json:"updated"`
	Failed  int          `json:"failed"`
	Blocked int          `json:"blocked"`
	Partial int          `json:"partial"`
}

type ItemResult struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Status  string `json:"status"` // "success", "skipped", "error", "blocked", "partial"
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
	"instanceType":  {"/api/library/instance-types", "instanceType", []string{"id", "dateCreated", "lastUpdated", "account", "instanceTypeLayouts"}},
	"catalogItem":   {"/api/catalog-item-types", "catalogItemType", []string{"id", "dateCreated", "lastUpdated", "account"}},
	"blueprint":     {"/api/blueprints", "blueprint", []string{"id", "dateCreated", "lastUpdated", "account", "visibility"}},
	"credential":    {"/api/credentials", "credential", []string{"id", "dateCreated", "lastUpdated", "account"}},
	"storageBucket": {"/api/storage-buckets", "storageBucket", []string{"id", "dateCreated", "lastUpdated"}},
	"cypher":        {"/api/cypher", "cypher", []string{"id", "dateCreated", "lastUpdated"}},
	"network":       {"/api/networks", "network", []string{"id", "dateCreated", "lastUpdated", "zone"}},
	"networkPool":   {"/api/networks/pools", "networkPool", []string{"id", "dateCreated", "lastUpdated"}},
	"networkDomain": {"/api/networks/domains", "networkDomain", []string{"id", "dateCreated", "lastUpdated"}},
	"virtualImage":  {"/api/virtual-images", "virtualImage", []string{"id", "dateCreated", "lastUpdated", "account", "storageProvider"}},
}

func Run(req MigrateRequest) *MigrateResult {
	result := &MigrateResult{}
	dst := morpheus.NewClient(req.Destination.URL, req.Destination.Token, req.Destination.SkipTLS)
	dst.HTTPDebug = req.HttpDebug

	var src *morpheus.Client
	if strings.TrimSpace(req.Source.URL) != "" && strings.TrimSpace(req.Source.Token) != "" {
		src = morpheus.NewClient(req.Source.URL, req.Source.Token, req.Source.SkipTLS)
		src.HTTPDebug = req.HttpDebug
	}

	state := newAutomationState()
	items := sortItemsForMigration(req.Items)

	for _, item := range items {
		switch item.Type {
		case "task":
			appendItemResult(result, migrateTaskWithAutomation(src, dst, item, state))
			continue
		case "workflow":
			appendItemResult(result, migrateWorkflowWithAutomation(src, dst, item, state))
			continue
		case "input":
			appendItemResult(result, migrateInputWithAutomation(src, dst, item, state))
			continue
		case "form":
			appendItemResult(result, migrateFormWithAutomation(src, dst, item, state))
			continue
		default:
		}

		spec, ok := endpointMap[item.Type]
		if !ok {
			result.Results = append(result.Results, ItemResult{
				Name:    item.Name,
				Type:    item.Type,
				Status:  "skipped",
				Message: fmt.Sprintf("Migration of type '%s' not yet supported", item.Type),
			})
			result.Failed++
			continue
		}

		payload, err := buildPayload(item.RawJSON, spec)
		if err != nil {
			appendItemResult(result, ItemResult{
				Name:    item.Name,
				Type:    item.Type,
				Status:  "error",
				Message: fmt.Sprintf("Failed to build payload: %v", err),
			})
			continue
		}

		_, err = dst.PostRaw(spec.endpoint, payload)
		if err != nil {
			if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "422") {
				appendItemResult(result, ItemResult{
					Name:    item.Name,
					Type:    item.Type,
					Status:  "skipped",
					Message: "Already exists on destination",
				})
				continue
			}
			appendItemResult(result, ItemResult{
				Name:    item.Name,
				Type:    item.Type,
				Status:  "error",
				Message: err.Error(),
			})
			continue
		}

		appendItemResult(result, ItemResult{
			Name:    item.Name,
			Type:    item.Type,
			Status:  "success",
			Outcome: "created",
		})
	}

	return result
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
