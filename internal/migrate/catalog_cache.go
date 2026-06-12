package migrate

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/cuxtud/morpheus-migration-utility/internal/morpheus"
)

// Destination indexes cached for the migration run (safe for parallel catalog workers).
type catalogDestCache struct {
	mu sync.RWMutex

	catalogLoaded bool
	catalogByCode map[string]int64
	catalogByName map[string]int64

	formLoaded bool
	formByCode map[string]int64
	formByName map[string]int64
	srcFormToDest map[int64]int64

	groupLoaded bool
	groupByName map[string]string

	cloudLoaded bool
	cloudByName map[string]int64
	cloudByCode map[string]int64
	cloudByID   map[int64]string

	planLoaded bool
	planByCode  map[string]int64
	planByID    map[int64]int64

	networksByCloud map[int64][]map[string]interface{}

	instanceTypeLoaded bool
	instanceTypeByCode map[string]int64

	virtualImageByName map[string]int64
}

func (s *automationState) destCache() *catalogDestCache {
	if s.catalogCache == nil {
		s.catalogCache = &catalogDestCache{
			catalogByCode: map[string]int64{},
			catalogByName: map[string]int64{},
			formByCode:     map[string]int64{},
			formByName:     map[string]int64{},
			srcFormToDest:  map[int64]int64{},
			groupByName:   map[string]string{},
			cloudByName:     map[string]int64{},
			cloudByCode:     map[string]int64{},
			cloudByID:       map[int64]string{},
			planByCode:      map[string]int64{},
			planByID:        map[int64]int64{},
			networksByCloud:    map[int64][]map[string]interface{}{},
			instanceTypeByCode: map[string]int64{},
			virtualImageByName: map[string]int64{},
		}
	}
	return s.catalogCache
}

func (s *automationState) ensureDestCatalogIndex(dst *morpheus.Client) error {
	c := s.destCache()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.catalogLoaded {
		return nil
	}
	raws, err := paginateList(dst, "/api/catalog-item-types", "catalogItemTypes")
	if err != nil {
		return err
	}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		id := intFromAny(row["id"])
		if id <= 0 {
			continue
		}
		if code := strings.ToLower(strings.TrimSpace(stringFromAny(row["code"]))); code != "" {
			c.catalogByCode[code] = id
		}
		if name := strings.ToLower(strings.TrimSpace(stringFromAny(row["name"]))); name != "" {
			c.catalogByName[name] = id
		}
	}
	c.catalogLoaded = true
	return nil
}

func (s *automationState) findDestCatalogID(dst *morpheus.Client, code, name string) (int64, error) {
	if err := s.ensureDestCatalogIndex(dst); err != nil {
		return 0, err
	}
	c := s.destCache()
	c.mu.RLock()
	defer c.mu.RUnlock()
	if want := strings.ToLower(strings.TrimSpace(code)); want != "" {
		if id := c.catalogByCode[want]; id > 0 {
			return id, nil
		}
	}
	if want := strings.ToLower(strings.TrimSpace(name)); want != "" {
		if id := c.catalogByName[want]; id > 0 {
			return id, nil
		}
	}
	return 0, nil
}

func (s *automationState) ensureDestFormIndex(dst *morpheus.Client) error {
	c := s.destCache()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.formLoaded {
		return nil
	}
	raws, err := paginateList(dst, "/api/library/option-type-forms", "optionTypeForms")
	if err != nil {
		return err
	}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		id := intFromAny(row["id"])
		if id <= 0 {
			continue
		}
		if name := strings.ToLower(strings.TrimSpace(stringFromAny(row["name"]))); name != "" {
			c.formByName[name] = id
		}
		if code := strings.ToLower(strings.TrimSpace(stringFromAny(row["code"]))); code != "" {
			c.formByCode[code] = id
		}
	}
	c.formLoaded = true
	return nil
}

func (s *automationState) findDestFormID(dst *morpheus.Client, code, name string) int64 {
	_ = s.ensureDestFormIndex(dst)
	wantName := strings.ToLower(strings.TrimSpace(name))
	wantCode := strings.ToLower(strings.TrimSpace(code))
	c := s.destCache()
	c.mu.RLock()
	defer c.mu.RUnlock()
	if wantName != "" {
		if id := c.formByName[wantName]; id > 0 {
			return id
		}
	}
	if wantCode != "" {
		return c.formByCode[wantCode]
	}
	return 0
}

func (s *automationState) destFormIDForSource(srcFormID int64) int64 {
	if s == nil || srcFormID <= 0 {
		return 0
	}
	c := s.destCache()
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.srcFormToDest[srcFormID]
}

// registerDestForm records a destination form for catalog linking (even when form update is blocked).
func (s *automationState) registerDestForm(destID int64, code, name string, srcFormID int64) {
	if s == nil || destID <= 0 {
		return
	}
	c := s.destCache()
	c.mu.Lock()
	defer c.mu.Unlock()
	if name := strings.ToLower(strings.TrimSpace(name)); name != "" {
		c.formByName[name] = destID
	}
	if code := strings.ToLower(strings.TrimSpace(code)); code != "" {
		c.formByCode[code] = destID
	}
	if srcFormID > 0 {
		c.srcFormToDest[srcFormID] = destID
	}
}

func (s *automationState) refreshDestFormIndex(dst *morpheus.Client) error {
	if s == nil {
		return nil
	}
	c := s.destCache()
	c.mu.Lock()
	c.formLoaded = false
	c.formByName = map[string]int64{}
	c.formByCode = map[string]int64{}
	// preserve srcFormToDest across refresh
	c.mu.Unlock()
	return s.ensureDestFormIndex(dst)
}

func (s *automationState) ensureDestGroupIndex(dst *morpheus.Client) error {
	c := s.destCache()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.groupLoaded {
		return nil
	}
	raws, err := paginateList(dst, "/api/groups", "groups")
	if err != nil {
		return err
	}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		id := strings.TrimSpace(stringFromAny(row["id"]))
		if id == "" {
			continue
		}
		if name := strings.ToLower(strings.TrimSpace(stringFromAny(row["name"]))); name != "" {
			c.groupByName[name] = id
		}
	}
	c.groupLoaded = true
	return nil
}

func (s *automationState) findDestGroupIDCached(dst *morpheus.Client, name string) (string, error) {
	if err := s.ensureDestGroupIndex(dst); err != nil {
		return "", err
	}
	want := strings.ToLower(strings.TrimSpace(name))
	if want == "" {
		return "", nil
	}
	c := s.destCache()
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.groupByName[want], nil
}

func (s *automationState) ensureDestCloudIndex(dst *morpheus.Client) error {
	c := s.destCache()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cloudLoaded {
		return nil
	}
	raws, err := paginateList(dst, "/api/zones", "zones")
	if err != nil {
		return err
	}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		id := intFromAny(row["id"])
		if id <= 0 {
			continue
		}
		displayName := strings.TrimSpace(stringFromAny(row["name"]))
		if displayName != "" {
			c.cloudByID[id] = displayName
			c.cloudByName[strings.ToLower(displayName)] = id
		}
		for _, codeKey := range []string{"code", "zoneCode"} {
			if code := strings.ToLower(strings.TrimSpace(stringFromAny(row[codeKey]))); code != "" {
				c.cloudByCode[code] = id
			}
		}
	}
	c.cloudLoaded = true
	return nil
}

func (s *automationState) findDestCloudIDCached(dst *morpheus.Client, name, code string) (int64, string, error) {
	if err := s.ensureDestCloudIndex(dst); err != nil {
		return 0, "", err
	}
	c := s.destCache()
	c.mu.RLock()
	defer c.mu.RUnlock()
	if want := strings.ToLower(strings.TrimSpace(name)); want != "" {
		if id := c.cloudByName[want]; id > 0 {
			return id, name, nil
		}
	}
	if want := strings.ToLower(strings.TrimSpace(code)); want != "" {
		if id := c.cloudByCode[want]; id > 0 {
			return id, code, nil
		}
	}
	return 0, "", nil
}

func (s *automationState) destCloudDisplayName(dst *morpheus.Client, id int64) string {
	if id <= 0 {
		return ""
	}
	_ = s.ensureDestCloudIndex(dst)
	c := s.destCache()
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cloudByID[id]
}

func (s *automationState) ensureDestServicePlanIndex(dst *morpheus.Client) error {
	c := s.destCache()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.planLoaded {
		return nil
	}
	raws, err := paginateList(dst, "/api/service-plans", "servicePlans")
	if err != nil {
		return err
	}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		id := intFromAny(row["id"])
		if id <= 0 {
			continue
		}
		c.planByID[id] = id
		if code := strings.ToLower(strings.TrimSpace(stringFromAny(row["code"]))); code != "" {
			c.planByCode[code] = id
		}
	}
	c.planLoaded = true
	return nil
}

func (s *automationState) findDestServicePlanIDCached(dst *morpheus.Client, code string, id int64) (int64, string, error) {
	if err := s.ensureDestServicePlanIndex(dst); err != nil {
		return 0, "", err
	}
	c := s.destCache()
	c.mu.RLock()
	defer c.mu.RUnlock()
	if want := strings.ToLower(strings.TrimSpace(code)); want != "" {
		if pid := c.planByCode[want]; pid > 0 {
			return pid, code, nil
		}
	}
	if id > 0 {
		if pid := c.planByID[id]; pid > 0 {
			return pid, "", nil
		}
	}
	return 0, "", nil
}

func (s *automationState) listDestNetworksCached(dst *morpheus.Client, cloudID int64) ([]map[string]interface{}, error) {
	if cloudID <= 0 {
		return nil, nil
	}
	c := s.destCache()
	c.mu.RLock()
	if rows, ok := c.networksByCloud[cloudID]; ok {
		c.mu.RUnlock()
		return rows, nil
	}
	c.mu.RUnlock()

	rows, err := listDestNetworksForCloud(dst, cloudID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.networksByCloud[cloudID] = rows
	c.mu.Unlock()
	return rows, nil
}

func (s *automationState) ensureDestInstanceTypeIndex(dst *morpheus.Client) error {
	c := s.destCache()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.instanceTypeLoaded {
		return nil
	}
	raws, err := paginateList(dst, "/api/library/instance-types", "instanceTypes")
	if err != nil {
		raws, err = paginateList(dst, "/api/instance-types", "instanceTypes")
		if err != nil {
			return err
		}
	}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		id := intFromAny(row["id"])
		if id <= 0 {
			continue
		}
		if code := strings.ToLower(strings.TrimSpace(stringFromAny(row["code"]))); code != "" {
			c.instanceTypeByCode[code] = id
		}
	}
	c.instanceTypeLoaded = true
	return nil
}

func (s *automationState) findDestInstanceTypeIDCached(dst *morpheus.Client, code string) (int64, bool, error) {
	want := strings.ToLower(strings.TrimSpace(code))
	if want == "" {
		return 0, false, nil
	}
	if err := s.ensureDestInstanceTypeIndex(dst); err != nil {
		return 0, false, err
	}
	c := s.destCache()
	c.mu.RLock()
	defer c.mu.RUnlock()
	if id := c.instanceTypeByCode[want]; id > 0 {
		return id, true, nil
	}
	return 0, false, nil
}

func (s *automationState) findDestVirtualImageIDCached(dst *morpheus.Client, name string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, nil
	}
	key := strings.ToLower(name)
	c := s.destCache()
	c.mu.RLock()
	if id, ok := c.virtualImageByName[key]; ok {
		c.mu.RUnlock()
		return id, nil
	}
	c.mu.RUnlock()

	id, err := findDestinationVirtualImageIDByName(dst, name)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	c.virtualImageByName[key] = id
	c.mu.Unlock()
	return id, nil
}
