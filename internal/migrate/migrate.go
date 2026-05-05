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
	Failed  int          `json:"failed"`
}

type ItemResult struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Status  string `json:"status"` // "success", "skipped", "error"
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
	"task":          {"/api/tasks", "task", []string{"id", "dateCreated", "lastUpdated", "account"}},
	"workflow":      {"/api/task-sets", "taskSet", []string{"id", "dateCreated", "lastUpdated", "account"}},
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

	for _, item := range req.Items {
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
			result.Results = append(result.Results, ItemResult{
				Name:    item.Name,
				Type:    item.Type,
				Status:  "error",
				Message: fmt.Sprintf("Failed to build payload: %v", err),
			})
			result.Failed++
			continue
		}

		_, err = dst.PostRaw(spec.endpoint, payload)
		if err != nil {
			// check if duplicate
			if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "422") {
				result.Results = append(result.Results, ItemResult{
					Name:    item.Name,
					Type:    item.Type,
					Status:  "skipped",
					Message: "Already exists on destination",
				})
				continue
			}
			result.Results = append(result.Results, ItemResult{
				Name:    item.Name,
				Type:    item.Type,
				Status:  "error",
				Message: err.Error(),
			})
			result.Failed++
			continue
		}

		result.Results = append(result.Results, ItemResult{
			Name:   item.Name,
			Type:   item.Type,
			Status: "success",
		})
		result.Success++
	}

	return result
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
