package migrate

import (
	"strings"
	"sync"

	"github.com/cuxtud/morpheus-migration-utility/internal/morpheus"
)

// SourceSnapshot indexes discovery (and selected item) payloads for migration lookups.
type SourceSnapshot struct {
	mu sync.RWMutex

	byKey              map[itemKey]SelectedItem
	instanceTypeByCode map[string]int64
	cloudByCode        map[string]int64
}

// NewSourceSnapshot builds a lookup index from a persisted discovery result and/or selected items.
func NewSourceSnapshot(discovery *morpheus.DiscoveryResult, selected []SelectedItem) *SourceSnapshot {
	s := &SourceSnapshot{
		byKey:              map[itemKey]SelectedItem{},
		instanceTypeByCode: map[string]int64{},
		cloudByCode:        map[string]int64{},
	}
	if discovery != nil {
		for _, cat := range discovery.Categories {
			for _, item := range cat.Items {
				s.put(selectedItemFromDiscovery(cat.Name, item))
			}
		}
	}
	for _, it := range selected {
		s.put(it)
	}
	return s
}

func selectedItemFromDiscovery(category string, item morpheus.DiscoveryItem) SelectedItem {
	typ := normalizeType(item.Type)
	if typ == "" {
		typ = discoveryCategoryDefaultType(category)
	}
	return SelectedItem{
		Category: category,
		Type:     typ,
		ID:       item.ID,
		Name:     strings.TrimSpace(item.Name),
		RawJSON:  strings.TrimSpace(item.RawJSON),
	}
}

func discoveryCategoryDefaultType(category string) string {
	switch strings.TrimSpace(category) {
	case "Clouds":
		return "cloud"
	case "Inputs":
		return "input"
	case "Option Lists":
		return "optionList"
	case "Forms":
		return "form"
	case "Tasks":
		return "task"
	case "Workflows":
		return "workflow"
	case "Layouts":
		return "layout"
	case "Node Types":
		return "nodeType"
	case "Virtual Images":
		return "virtualImage"
	case "Catalog Items":
		return "catalogItem"
	case "Instance Types":
		return "instanceType"
	case "Groups":
		return "group"
	default:
		return ""
	}
}

func (s *SourceSnapshot) put(it SelectedItem) {
	if s == nil {
		return
	}
	it.Type = normalizeType(it.Type)
	if it.ID <= 0 || it.Type == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := keyForItem(it)
	if prev, ok := s.byKey[k]; ok && len(strings.TrimSpace(it.RawJSON)) == 0 {
		it.RawJSON = prev.RawJSON
	}
	if len(strings.TrimSpace(it.RawJSON)) > 0 {
		s.byKey[k] = it
		s.indexCodes(it)
	} else if _, ok := s.byKey[k]; !ok {
		s.byKey[k] = it
	}
}

func (s *SourceSnapshot) indexCodes(it SelectedItem) {
	obj := parseObject(it.RawJSON)
	if obj == nil {
		return
	}
	code := strings.ToLower(strings.TrimSpace(stringFromAny(obj["code"])))
	if code == "" {
		return
	}
	switch it.Type {
	case "instanceType":
		s.instanceTypeByCode[code] = it.ID
	case "cloud":
		s.cloudByCode[code] = it.ID
		if zc := strings.ToLower(strings.TrimSpace(stringFromAny(obj["zoneCode"]))); zc != "" {
			s.cloudByCode[zc] = it.ID
		}
	}
}

// Lookup returns a source item from the snapshot when available.
func (s *SourceSnapshot) Lookup(typ string, id int64) (SelectedItem, bool) {
	if s == nil || id <= 0 {
		return SelectedItem{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	it, ok := s.byKey[itemKey{Type: normalizeType(typ), ID: id}]
	if !ok || strings.TrimSpace(it.RawJSON) == "" {
		return SelectedItem{}, false
	}
	return it, true
}

// LookupObject parses RawJSON for a typed id from the snapshot.
func (s *SourceSnapshot) LookupObject(typ string, id int64) (map[string]interface{}, bool) {
	it, ok := s.Lookup(typ, id)
	if !ok {
		return nil, false
	}
	obj := parseObject(it.RawJSON)
	return obj, obj != nil
}

// FindInstanceTypeByCode resolves an instance type from the snapshot index.
func (s *SourceSnapshot) FindInstanceTypeByCode(code string) (SelectedItem, bool) {
	if s == nil {
		return SelectedItem{}, false
	}
	want := strings.ToLower(strings.TrimSpace(code))
	if want == "" {
		return SelectedItem{}, false
	}
	s.mu.RLock()
	id := s.instanceTypeByCode[want]
	s.mu.RUnlock()
	if id <= 0 {
		return SelectedItem{}, false
	}
	return s.Lookup("instanceType", id)
}

func catalogItemLooksComplete(obj map[string]interface{}) bool {
	if obj == nil {
		return false
	}
	if cfg, ok := obj["config"].(map[string]interface{}); ok && cfg != nil {
		return true
	}
	return strings.TrimSpace(stringFromAny(obj["instanceSpec"])) != ""
}

// CatalogObject returns catalog item JSON preferring selected/discovery data over live API.
func (s *SourceSnapshot) CatalogObject(src *morpheus.Client, item SelectedItem) (map[string]interface{}, error) {
	if obj := parseObject(item.RawJSON); catalogItemLooksComplete(obj) {
		return obj, nil
	}
	if item.ID > 0 {
		if obj, ok := s.LookupObject("catalogItem", item.ID); ok && catalogItemLooksComplete(obj) {
			return obj, nil
		}
	}
	return fetchFullCatalogItem(src, item.ID)
}

// CloudObject returns zone/cloud JSON preferring discovery data over live API.
func (s *SourceSnapshot) CloudObject(src *morpheus.Client, id int64) (map[string]interface{}, error) {
	if id <= 0 {
		return nil, nil
	}
	if obj, ok := s.LookupObject("cloud", id); ok {
		return obj, nil
	}
	return fetchFullCloud(src, id)
}

// InstanceTypeObject returns instance type JSON preferring discovery data over live API.
func (s *SourceSnapshot) InstanceTypeObject(src *morpheus.Client, item SelectedItem) (map[string]interface{}, error) {
	if obj := parseObject(item.RawJSON); obj != nil && strings.TrimSpace(stringFromAny(obj["code"])) != "" {
		if _, ok := obj["instanceTypeLayouts"]; ok {
			return obj, nil
		}
	}
	if item.ID > 0 {
		if obj, ok := s.LookupObject("instanceType", item.ID); ok {
			return obj, nil
		}
	}
	return fetchFullInstanceType(src, item.ID)
}

// FormObject returns form JSON preferring discovery data over live API.
func (s *SourceSnapshot) FormObject(src *morpheus.Client, item SelectedItem) (map[string]interface{}, error) {
	raw := parseObject(item.RawJSON)
	if raw != nil {
		obj := unwrapFormObject(raw)
		if len(obj) > 0 && strings.TrimSpace(stringFromAny(obj["code"])) != "" {
			return obj, nil
		}
	}
	if item.ID > 0 {
		if it, ok := s.Lookup("form", item.ID); ok {
			if obj := unwrapFormObject(parseObject(it.RawJSON)); len(obj) > 0 {
				return obj, nil
			}
		}
	}
	return fetchFullOptionTypeForm(src, item.ID)
}

func unwrapFormObject(raw map[string]interface{}) map[string]interface{} {
	if raw == nil {
		return nil
	}
	if wrapped, ok := raw["optionTypeForm"].(map[string]interface{}); ok && wrapped != nil {
		return wrapped
	}
	return raw
}

// ResolveSourceItem returns a dependency item from the snapshot or source API.
func (s *SourceSnapshot) ResolveSourceItem(src *morpheus.Client, typ string, id int64) (SelectedItem, error) {
	if s != nil {
		if it, ok := s.Lookup(typ, id); ok {
			return it, nil
		}
	}
	it, err := fetchSourceByIDLive(src, typ, id)
	if err != nil {
		return SelectedItem{}, err
	}
	if s != nil {
		s.put(it)
	}
	return it, nil
}

// ResolveInstanceTypeByCode finds an instance type by code from snapshot or source API.
func (s *SourceSnapshot) ResolveInstanceTypeByCode(src *morpheus.Client, code string) (SelectedItem, error) {
	if s != nil {
		if it, ok := s.FindInstanceTypeByCode(code); ok {
			return it, nil
		}
	}
	it, err := findSourceInstanceTypeByCodeLive(src, code)
	if err != nil {
		return SelectedItem{}, err
	}
	if s != nil {
		s.put(it)
	}
	return it, nil
}

// EnrichCatalogItem updates item RawJSON from snapshot when the payload is already complete.
func (s *SourceSnapshot) EnrichCatalogItem(src *morpheus.Client, it SelectedItem) (SelectedItem, error) {
	var obj map[string]interface{}
	var err error
	if s != nil {
		obj, err = s.CatalogObject(src, it)
	} else {
		obj, err = fetchFullCatalogItem(src, it.ID)
	}
	if err != nil {
		return it, err
	}
	it.RawJSON = mustJSON(obj)
	return it, nil
}

func (s *automationState) formObject(src *morpheus.Client, item SelectedItem) (map[string]interface{}, error) {
	if s != nil && s.sourceSnap != nil {
		return s.sourceSnap.FormObject(src, item)
	}
	return fetchFullOptionTypeForm(src, item.ID)
}
