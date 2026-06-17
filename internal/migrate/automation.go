package migrate

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/cuxtud/morpheus-migration-utility/internal/morpheus"
)

// automationState tracks destination IDs discovered during a migration run.
type automationState struct {
	mu sync.RWMutex

	destTaskNameToID    map[string]int64
	destWorkflowKeyToID map[string]int64 // name + "\x00" + type
	destOptionCodeToID  map[string]int64
	optionTypesLoadErr  string
	progress            ProgressFunc
	catalogCache        *catalogDestCache
	sourceSnap          *SourceSnapshot
}

func newAutomationState(snap *SourceSnapshot) *automationState {
	return &automationState{
		destTaskNameToID:    map[string]int64{},
		destWorkflowKeyToID: map[string]int64{},
		destOptionCodeToID:  map[string]int64{},
		sourceSnap:          snap,
	}
}

func sortItemsForMigration(items []SelectedItem) []SelectedItem {
	out := make([]SelectedItem, len(items))
	copy(out, items)
	sort.SliceStable(out, func(i, j int) bool {
		return itemTypeOrder(out[i].Type) < itemTypeOrder(out[j].Type)
	})
	return out
}

func itemTypeOrder(t string) int {
	switch normalizeType(t) {
	case "credential":
		return -3
	case "group":
		return -2
	case "cloud":
		return -1
	case "nodeType":
		return 0
	case "task":
		return 1
	case "workflow":
		return 2
	case "layout":
		return 3
	case "instanceType":
		return 4
	case "catalogItem":
		return 8
	case "optionList":
		return 5
	case "input":
		return 6
	case "form":
		return 7
	default:
		return 99
	}
}

func (s *automationState) refreshDestTasks(dst *morpheus.Client) error {
	raws, err := paginateList(dst, "/api/tasks", "tasks")
	if err != nil {
		return err
	}
	next := map[string]int64{}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		id := intFromAny(row["id"])
		name := stringFromAny(row["name"])
		if name != "" && id > 0 {
			next[name] = id
		}
	}
	s.mu.Lock()
	s.destTaskNameToID = next
	s.mu.Unlock()
	return nil
}

func (s *automationState) refreshDestWorkflows(dst *morpheus.Client) error {
	raws, err := paginateList(dst, "/api/task-sets", "taskSets")
	if err != nil {
		return err
	}
	next := map[string]int64{}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		id := intFromAny(row["id"])
		name := stringFromAny(row["name"])
		wt := stringFromAny(row["type"])
		if name != "" && id > 0 {
			next[workflowKey(name, wt)] = id
		}
	}
	s.mu.Lock()
	s.destWorkflowKeyToID = next
	s.mu.Unlock()
	return nil
}

func (s *automationState) destTaskID(name string) int64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.destTaskNameToID[name]
}

func (s *automationState) destTaskMapCopy() map[string]int64 {
	if s == nil {
		return map[string]int64{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]int64, len(s.destTaskNameToID))
	for k, v := range s.destTaskNameToID {
		out[k] = v
	}
	return out
}

func (s *automationState) destWorkflowID(name, wtype string) int64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.destWorkflowKeyToID[workflowKey(name, wtype)]
}

func (s *automationState) destWorkflowIDByName(name string) int64 {
	if s == nil || name == "" {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for key, id := range s.destWorkflowKeyToID {
		parts := strings.SplitN(key, "\x00", 2)
		if len(parts) > 0 && strings.EqualFold(parts[0], name) {
			return id
		}
	}
	return 0
}

func (s *automationState) destOptionTypeID(code string) int64 {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.destOptionCodeToID[code]
}

func (s *automationState) setDestOptionTypeID(code string, id int64) {
	if s == nil || code == "" || id <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.destOptionCodeToID[code] = id
}

func (s *automationState) destOptionTypeMapCopy() map[string]int64 {
	if s == nil {
		return map[string]int64{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]int64, len(s.destOptionCodeToID))
	for k, v := range s.destOptionCodeToID {
		out[k] = v
	}
	return out
}

func (s *automationState) optionTypesLoadError() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.optionTypesLoadErr
}

func workflowKey(name, wtype string) string {
	return name + "\x00" + wtype
}

func (s *automationState) loadDestOptionTypes(dst *morpheus.Client) {
	if s == nil {
		return
	}
	s.mu.RLock()
	loaded := len(s.destOptionCodeToID) > 0
	s.mu.RUnlock()
	if loaded {
		return
	}
	s.reloadDestOptionTypes(dst)
}

func (s *automationState) reloadDestOptionTypes(dst *morpheus.Client) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.destOptionCodeToID = map[string]int64{}
	s.optionTypesLoadErr = ""
	s.fetchOptionTypesFromDestinationLocked(dst)
}

func (s *automationState) fetchOptionTypesFromDestinationLocked(dst *morpheus.Client) {
	candidates := []struct {
		path    string
		dataKey string
	}{
		{"/api/library/option-types", "optionTypes"},
		{"/api/options/types", "optionTypes"},
		{"/api/option-types", "optionTypes"},
	}
	for _, c := range candidates {
		raws, err := paginateList(dst, c.path, c.dataKey)
		if err != nil {
			continue
		}
		for _, raw := range raws {
			var row map[string]interface{}
			if json.Unmarshal(raw, &row) != nil {
				continue
			}
			id := intFromAny(row["id"])
			code := strings.TrimSpace(stringFromAny(row["code"]))
			if code != "" && id > 0 {
				s.destOptionCodeToID[code] = id
			}
		}
		if len(s.destOptionCodeToID) > 0 {
			return
		}
	}
	s.optionTypesLoadErr = "could not list option types on destination (optionTypes may be omitted)"
}

func paginateList(c *morpheus.Client, basePath, dataKey string) ([][]byte, error) {
	var all [][]byte
	offset := 0
	max := 50
	for {
		path := fmt.Sprintf("%s?max=%d&offset=%d", basePath, max, offset)
		body, err := c.GetRaw(path)
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
		for _, it := range items {
			all = append(all, []byte(it))
		}
		if len(items) < max {
			break
		}
		offset += max
	}
	return all, nil
}

// ensureNestedWorkflowTasks migrates tasks referenced by the workflow when they are missing on the destination
// (nested task payloads come from taskSetTasks). Repository-backed tasks run full integration checks via migrateTaskWithAutomation.
func ensureNestedWorkflowTasks(src, dst *morpheus.Client, wf map[string]interface{}, wfName string, state *automationState) *ItemResult {
	tst, ok := wf["taskSetTasks"].([]interface{})
	if !ok || len(tst) == 0 {
		// Inputs-only workflows (operation/provision) may have no tasks; still migrate optionTypes.
		return nil
	}

	taskByName := map[string]map[string]interface{}{}
	for _, e := range tst {
		em, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		tm, ok := em["task"].(map[string]interface{})
		if !ok {
			continue
		}
		tn := strings.TrimSpace(stringFromAny(tm["name"]))
		if tn == "" {
			continue
		}
		if _, exists := taskByName[tn]; !exists {
			taskByName[tn] = tm
		}
	}

	type row struct {
		order int64
		name  string
	}
	var rows []row
	for _, e := range tst {
		em, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		tm, ok := em["task"].(map[string]interface{})
		if !ok {
			continue
		}
		tn := strings.TrimSpace(stringFromAny(tm["name"]))
		if tn == "" {
			continue
		}
		rows = append(rows, row{order: intFromAny(em["taskOrder"]), name: tn})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].order != rows[j].order {
			return rows[i].order < rows[j].order
		}
		return i < j
	})

	if len(rows) == 0 {
		return nil
	}

	done := map[string]struct{}{}
	for _, r := range rows {
		if _, ok := done[r.name]; ok {
			continue
		}
		done[r.name] = struct{}{}

		if state.destTaskID(r.name) > 0 {
			continue
		}

		tm, ok := taskByName[r.name]
		if !ok {
			return &ItemResult{
				Name: wfName, Type: "workflow", Status: "blocked",
				Message: fmt.Sprintf("workflow references task %q but nested task payload was not found under taskSetTasks", r.name),
			}
		}

		raw, err := json.Marshal(tm)
		if err != nil {
			return &ItemResult{Name: wfName, Type: "workflow", Status: "error", Message: fmt.Sprintf("task %q: %v", r.name, err)}
		}

		item := SelectedItem{
			Category: "Tasks",
			Type:     "task",
			ID:       intFromAny(tm["id"]),
			Name:     r.name,
			RawJSON:  string(raw),
		}

		res := migrateTaskWithAutomation(src, dst, item, state)
		switch res.Status {
		case "success", "skipped":
			if err := state.refreshDestTasks(dst); err != nil {
				return &ItemResult{Name: wfName, Type: "workflow", Status: "error", Message: fmt.Sprintf("after migrating task %q: %v", r.name, err)}
			}
		case "blocked":
			return &ItemResult{Name: wfName, Type: "workflow", Status: "blocked", Message: fmt.Sprintf("workflow dependency (task %q): %s", r.name, res.Message)}
		case "partial":
			return &ItemResult{Name: wfName, Type: "workflow", Status: "blocked", Message: fmt.Sprintf("workflow dependency (task %q): %s", r.name, res.Message)}
		case "error":
			return &ItemResult{Name: wfName, Type: "workflow", Status: "error", Message: fmt.Sprintf("workflow dependency (task %q): %s", r.name, res.Message)}
		default:
			return &ItemResult{Name: wfName, Type: "workflow", Status: "error", Message: fmt.Sprintf("workflow dependency (task %q): unexpected status %q", r.name, res.Status)}
		}
	}
	return nil
}

func ensureMissingOptionTypes(src, dst *morpheus.Client, wf map[string]interface{}, wfName string, state *automationState) *ItemResult {
	arr, ok := wf["optionTypes"].([]interface{})
	if !ok || len(arr) == 0 {
		return nil
	}

	state.reloadDestOptionTypes(dst)

	for _, e := range arr {
		om, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		code := strings.TrimSpace(stringFromAny(om["code"]))
		if code == "" {
			code = strings.TrimSpace(stringFromAny(om["name"]))
		}
		if code == "" {
			continue
		}
		if state.destOptionTypeID(code) > 0 {
			continue
		}

		id, err := createOptionTypeOnDestination(src, dst, om, code)
		if err != nil {
			return &ItemResult{Name: wfName, Type: "workflow", Status: "blocked", Message: fmt.Sprintf("could not create inputs field %q on destination: %v", code, err)}
		}
		state.setDestOptionTypeID(code, id)
	}

	state.reloadDestOptionTypes(dst)
	return nil
}

func migrateInputWithAutomation(src, dst *morpheus.Client, item SelectedItem, state *automationState) ItemResult {
	name := strings.TrimSpace(item.Name)
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(item.RawJSON), &raw); err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "error", Message: fmt.Sprintf("invalid input json: %v", err)}
	}

	obj := raw
	if wrapped, ok := raw["optionType"].(map[string]interface{}); ok && wrapped != nil {
		obj = wrapped
	}

	if n := strings.TrimSpace(stringFromAny(obj["name"])); n != "" {
		name = n
	}
	code := strings.TrimSpace(stringFromAny(obj["code"]))
	if code == "" {
		code = strings.TrimSpace(stringFromAny(obj["fieldName"]))
	}
	if code == "" {
		code = strings.TrimSpace(name)
	}

	state.reportStep(fmt.Sprintf("Checking if input %q exists on destination", name))
	state.reloadDestOptionTypes(dst)
	if existingID := state.destOptionTypeID(code); existingID > 0 {
		state.reportStep(fmt.Sprintf("Input %q found on destination — updating", name))
		payload, err := buildOptionTypePayload(src, dst, obj, code)
		if err != nil {
			return ItemResult{Name: name, Type: item.Type, Status: "blocked", Message: fmt.Sprintf("prepare input payload: %v", err)}
		}
		_, err = dst.PutRaw(fmt.Sprintf("/api/library/option-types/%d", existingID), payload)
		if err != nil {
			return ItemResult{Name: name, Type: item.Type, Status: "error", Message: fmt.Sprintf("update input: %v", err)}
		}
		state.reloadDestOptionTypes(dst)
		return ItemResult{Name: name, Type: item.Type, Status: "success", Outcome: "updated", Message: "Updated input on destination"}
	}

	state.reportStep(fmt.Sprintf("Creating input %q on destination", name))
	_, err := createOptionTypeOnDestination(src, dst, obj, code)
	if err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "blocked", Message: fmt.Sprintf("create input: %v", err)}
	}
	state.reloadDestOptionTypes(dst)
	return ItemResult{Name: name, Type: item.Type, Status: "success", Outcome: "created", Message: "Created input on destination"}
}

func migrateFormWithAutomation(src, dst *morpheus.Client, item SelectedItem, state *automationState) ItemResult {
	name := strings.TrimSpace(item.Name)
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(item.RawJSON), &raw); err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "error", Message: fmt.Sprintf("invalid form json: %v", err)}
	}

	obj := raw
	if wrapped, ok := raw["optionTypeForm"].(map[string]interface{}); ok && wrapped != nil {
		obj = wrapped
	}

	if src != nil && state != nil {
		if fresh, err := state.formObject(src, item); err == nil && fresh != nil {
			obj = fresh
		}
	}

	if n := strings.TrimSpace(stringFromAny(obj["name"])); n != "" {
		name = n
	}

	formCode := strings.TrimSpace(stringFromAny(obj["code"]))
	if formCode == "" {
		return ItemResult{Name: name, Type: item.Type, Status: "blocked", Message: "form has empty code — cannot migrate"}
	}

	srcFormID := intFromAny(obj["id"])
	existingID := findOptionTypeFormID(dst, formCode, name)

	if err := ensureFormFieldGroupLibraryInputs(src, dst, state, obj); err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "blocked", Message: fmt.Sprintf("prepare form inputs: %v", err)}
	}
	destForm, err := loadDestinationOptionTypeForm(dst, existingID)
	if err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "error", Message: fmt.Sprintf("load destination form: %v", err)}
	}

	payload, err := buildOptionTypeFormPayload(src, dst, obj, destForm)
	if err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "blocked", Message: fmt.Sprintf("prepare form: %v", err)}
	}

	if existingID > 0 {
		if !formNeedsUpdate(src, dst, obj, existingID) {
			if state != nil {
				state.registerDestForm(existingID, formCode, name, srcFormID)
			}
			return ItemResult{Name: name, Type: item.Type, Status: "skipped", Message: "Form already exists and matches source"}
		}
		outcome, msg, err := putOptionTypeForm(dst, src, obj, existingID, formCode, name)
		if err != nil {
			return ItemResult{Name: name, Type: item.Type, Status: "error", Message: err.Error()}
		}
		if state != nil {
			state.registerDestForm(existingID, formCode, name, srcFormID)
		}
		return ItemResult{Name: name, Type: item.Type, Status: "success", Outcome: outcome, Message: msg}
	}

	body, err := dst.PostRaw("/api/library/option-type-forms", payload)
	if err != nil {
		if isDuplicateErr(err) && name != "" {
			existingID = findOptionTypeFormIDByName(dst, name)
			if existingID > 0 {
				if !formNeedsUpdate(src, dst, obj, existingID) {
					if state != nil {
						state.registerDestForm(existingID, formCode, name, srcFormID)
					}
					return ItemResult{Name: name, Type: item.Type, Status: "skipped", Message: "Form already exists and matches source"}
				}
				outcome, msg, putErr := putOptionTypeForm(dst, src, obj, existingID, formCode, name)
				if putErr != nil {
					return ItemResult{Name: name, Type: item.Type, Status: "error", Message: putErr.Error()}
				}
				if state != nil {
					state.registerDestForm(existingID, formCode, name, srcFormID)
				}
				return ItemResult{Name: name, Type: item.Type, Status: "success", Outcome: outcome, Message: msg}
			}
		}
		return ItemResult{Name: name, Type: item.Type, Status: "blocked", Message: fmt.Sprintf("create form: %v", err)}
	}
	newID := parseOptionTypeFormIDFromResponse(body)
	if newID <= 0 {
		if name != "" && findOptionTypeFormIDByName(dst, name) > 0 {
			return ItemResult{Name: name, Type: item.Type, Status: "skipped", Message: "Form may already exist on destination"}
		}
	} else if state != nil {
		state.registerDestForm(newID, formCode, name, srcFormID)
	}
	return ItemResult{Name: name, Type: item.Type, Status: "success", Outcome: "created", Message: "Created form on destination"}
}

func fetchFullOptionTypeForm(src *morpheus.Client, id int64) (map[string]interface{}, error) {
	if src == nil || id <= 0 {
		return nil, fmt.Errorf("invalid form id")
	}
	body, err := src.GetRaw(fmt.Sprintf("/api/library/option-type-forms/%d", id))
	if err != nil {
		return nil, fmt.Errorf("fetch form from source: %v", err)
	}
	var wrap map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, err
	}
	raw, ok := wrap["optionTypeForm"]
	if !ok {
		return nil, fmt.Errorf("source response missing optionTypeForm")
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func fetchFullOptionType(src *morpheus.Client, id int64) (map[string]interface{}, error) {
	if src == nil || id <= 0 {
		return nil, fmt.Errorf("invalid input id")
	}
	body, err := src.GetRaw(fmt.Sprintf("/api/library/option-types/%d", id))
	if err != nil {
		return nil, fmt.Errorf("fetch input from source: %v", err)
	}
	var wrap map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, err
	}
	raw, ok := wrap["optionType"]
	if !ok {
		return nil, fmt.Errorf("source response missing optionType")
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// isFormLibraryInputRef reports field-group options that reference library inputs (not inline form fields).
func isFormLibraryInputRef(opt map[string]interface{}) bool {
	if ff, ok := opt["formField"].(bool); ok {
		return !ff
	}
	name := strings.TrimSpace(stringFromAny(opt["name"]))
	if name == "" {
		return false
	}
	return !isLikelyUUID(name)
}

// isFormLibraryInputIDRef reports a resolved field-group option that should be sent as {"id": N} only.
func isFormLibraryInputIDRef(opt map[string]interface{}) bool {
	if intFromAny(opt["id"]) <= 0 {
		return false
	}
	return strings.TrimSpace(stringFromAny(opt["type"])) == "" &&
		strings.TrimSpace(stringFromAny(opt["fieldName"])) == "" &&
		strings.TrimSpace(stringFromAny(opt["code"])) == ""
}

func ensureFormFieldGroupLibraryInputs(src, dst *morpheus.Client, state *automationState, form map[string]interface{}) error {
	fgs, ok := form["fieldGroups"].([]interface{})
	if !ok {
		return nil
	}
	for _, g := range fgs {
		gm, ok := g.(map[string]interface{})
		if !ok {
			continue
		}
		opts, ok := gm["options"].([]interface{})
		if !ok {
			continue
		}
		newOpts := make([]interface{}, 0, len(opts))
		for _, e := range opts {
			om, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			if isFormLibraryInputIDRef(om) {
				newOpts = append(newOpts, om)
				continue
			}
			if !isFormLibraryInputRef(om) {
				newOpts = append(newOpts, om)
				continue
			}
			destID, err := ensureLibraryInputOnDestination(src, dst, state, om)
			if err != nil {
				return err
			}
			if destID > 0 {
				newOpts = append(newOpts, map[string]interface{}{"id": destID})
			}
		}
		gm["options"] = newOpts
	}
	return nil
}

func ensureLibraryInputOnDestination(src, dst *morpheus.Client, state *automationState, ref map[string]interface{}) (int64, error) {
	code := strings.TrimSpace(stringFromAny(ref["code"]))
	if code == "" {
		code = strings.TrimSpace(stringFromAny(ref["fieldName"]))
	}

	state.reloadDestOptionTypes(dst)
	if code != "" {
		if id := state.destOptionTypeID(code); id > 0 {
			return id, nil
		}
	}

	inputObj := ref
	srcID := intFromAny(ref["id"])
	if srcID > 0 && src != nil {
		fresh, err := fetchFullOptionType(src, srcID)
		if err != nil {
			return 0, err
		}
		inputObj = fresh
	}
	if code == "" {
		code = strings.TrimSpace(stringFromAny(inputObj["code"]))
	}
	if code == "" {
		code = strings.TrimSpace(stringFromAny(inputObj["fieldName"]))
	}
	if code == "" {
		return 0, fmt.Errorf("library input missing code")
	}

	if id := state.destOptionTypeID(code); id > 0 {
		return id, nil
	}

	label := strings.TrimSpace(stringFromAny(inputObj["name"]))
	if label == "" {
		label = code
	}
	state.reportStep(fmt.Sprintf("Creating input %q for form", label))
	id, err := createOptionTypeOnDestination(src, dst, inputObj, code)
	if err != nil {
		return 0, err
	}
	state.setDestOptionTypeID(code, id)
	return id, nil
}

func resolveDestinationFormID(src, dst *morpheus.Client, state *automationState, ref map[string]interface{}) (formCode, formName string, destID int64) {
	formCode = strings.TrimSpace(stringFromAny(ref["code"]))
	formName = strings.TrimSpace(stringFromAny(ref["name"]))
	srcFormID := intFromAny(ref["id"])

	if state != nil && srcFormID > 0 {
		if destID = state.destFormIDForSource(srcFormID); destID > 0 {
			return formCode, formName, destID
		}
	}

	lookup := func(code, name string) int64 {
		var id int64
		if state != nil {
			id = state.findDestFormID(dst, code, name)
		}
		if id <= 0 {
			id = findOptionTypeFormID(dst, code, name)
			if id > 0 && state != nil {
				state.registerDestForm(id, code, name, srcFormID)
			}
		}
		return id
	}

	destID = lookup(formCode, formName)
	if destID > 0 {
		return formCode, formName, destID
	}

	formObj := resolveSourceFormObject(src, state, srcFormID, formCode, formName)
	if formObj != nil {
		if formCode == "" {
			formCode = strings.TrimSpace(stringFromAny(formObj["code"]))
		}
		if formName == "" {
			formName = strings.TrimSpace(stringFromAny(formObj["name"]))
		}
		if destID = lookup(formCode, formName); destID > 0 {
			return formCode, formName, destID
		}
	}

	// Catalog payloads sometimes embed a display label that differs from the library form name.
	for _, n := range uniqueNonEmptyStrings(formName, strings.TrimSpace(stringFromAny(ref["name"]))) {
		if destID = lookup("", n); destID > 0 {
			return formCode, n, destID
		}
	}
	// Code-only lookup when no name is available (never match a different form by code alone).
	if strings.TrimSpace(formName) == "" && strings.TrimSpace(stringFromAny(ref["name"])) == "" {
		for _, c := range uniqueNonEmptyStrings(formCode, strings.TrimSpace(stringFromAny(ref["code"]))) {
			if destID = lookup(c, ""); destID > 0 {
				return c, formName, destID
			}
		}
	}

	return formCode, formName, 0
}

func resolveSourceFormObject(src *morpheus.Client, state *automationState, srcFormID int64, code, name string) map[string]interface{} {
	if state != nil && state.sourceSnap != nil && srcFormID > 0 {
		if it, ok := state.sourceSnap.Lookup("form", srcFormID); ok {
			if obj := unwrapFormObject(parseObject(it.RawJSON)); obj != nil {
				return obj
			}
		}
	}
	if src != nil && srcFormID > 0 {
		if obj, err := fetchFullOptionTypeForm(src, srcFormID); err == nil && obj != nil {
			return obj
		}
	}
	if state != nil && state.sourceSnap != nil && (code != "" || name != "") {
		item := SelectedItem{Type: "form", ID: srcFormID, Name: name, RawJSON: mustJSON(map[string]interface{}{
			"id": srcFormID, "name": name, "code": code,
		})}
		if obj, err := state.sourceSnap.FormObject(src, item); err == nil && obj != nil {
			return obj
		}
	}
	return nil
}

func walkFormOptions(form map[string]interface{}, fn func(groupCode string, opt map[string]interface{})) {
	if opts, ok := form["options"].([]interface{}); ok {
		for _, e := range opts {
			if om, ok := e.(map[string]interface{}); ok {
				fn("", om)
			}
		}
	}
	if fgs, ok := form["fieldGroups"].([]interface{}); ok {
		for _, g := range fgs {
			gm, ok := g.(map[string]interface{})
			if !ok {
				continue
			}
			gcode := strings.TrimSpace(stringFromAny(gm["code"]))
			if opts, ok := gm["options"].([]interface{}); ok {
				for _, e := range opts {
					if om, ok := e.(map[string]interface{}); ok {
						fn(gcode, om)
					}
				}
			}
		}
	}
}

func isLikelyUUID(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	if len(s) != 36 || strings.Count(s, "-") != 4 {
		return false
	}
	for _, c := range strings.ReplaceAll(s, "-", "") {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func formFieldCodeSlug(fieldName string) string {
	fieldName = strings.TrimSpace(fieldName)
	if fieldName == "" {
		return "field"
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(fieldName) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		case r == '_':
			b.WriteRune('_')
			lastUnderscore = true
		default:
			if b.Len() > 0 && !lastUnderscore {
				b.WriteRune('_')
				lastUnderscore = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "field"
	}
	return out
}

func uniqueFormFieldCode(base string, used map[string]struct{}) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "field"
	}
	for i := 0; ; i++ {
		var candidate string
		if i == 0 {
			candidate = base
		} else {
			candidate = fmt.Sprintf("%s_%d", base, i)
		}
		key := strings.ToLower(candidate)
		if _, dup := used[key]; !dup {
			used[key] = struct{}{}
			return candidate
		}
	}
}

func stripJSONNulls(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		for k, val := range x {
			if val == nil {
				delete(x, k)
				continue
			}
			x[k] = stripJSONNulls(val)
		}
		return x
	case []interface{}:
		out := make([]interface{}, 0, len(x))
		for _, el := range x {
			if el == nil {
				continue
			}
			out = append(out, stripJSONNulls(el))
		}
		return out
	default:
		return v
	}
}

// formOptMeta tracks one form field through code assignment and dependency normalization.
type formOptMeta struct {
	opt          map[string]interface{}
	groupCode    string
	oldCode      string
	fieldName    string
	typ          string
	displayOrder int64
	finalCode    string
}

func normalizeFormDepValue(s string, lookup func(string) string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.Index(s, ":"); i >= 0 {
		pre := strings.TrimSpace(s[:i])
		rest := s[i+1:]
		if m := lookup(pre); m != "" {
			return m + ":" + rest
		}
		return s
	}
	if strings.Contains(s, ",") {
		parts := strings.Split(s, ",")
		for i, p := range parts {
			pp := strings.TrimSpace(p)
			if m := lookup(pp); m != "" {
				parts[i] = m
			}
		}
		return strings.Join(parts, ", ")
	}
	if m := lookup(s); m != "" {
		return m
	}
	return s
}

func buildFormDepLookup(metas []formOptMeta) (lookup func(string) string) {
	m := map[string]string{}
	cloudField := ""
	cloudN := 0
	for _, meta := range metas {
		fn := strings.TrimSpace(meta.fieldName)
		if fn == "" {
			continue
		}
		m[strings.ToLower(fn)] = fn
		if meta.oldCode != "" {
			m[strings.ToLower(meta.oldCode)] = fn
		}
		if meta.finalCode != "" {
			m[strings.ToLower(meta.finalCode)] = fn
		}
		if strings.EqualFold(meta.typ, "cloud") {
			cloudN++
			cloudField = fn
		}
	}
	var singleCloud string
	if cloudN == 1 {
		singleCloud = cloudField
	}
	return func(token string) string {
		t := strings.TrimSpace(token)
		if t == "" {
			return ""
		}
		if v, ok := m[strings.ToLower(t)]; ok {
			return v
		}
		if strings.EqualFold(t, "cloud") && singleCloud != "" {
			return singleCloud
		}
		return ""
	}
}

func normalizeFormFieldDependencies(opt map[string]interface{}, lookup func(string) string) {
	for _, key := range []string{"dependsOnCode", "visibleOnCode", "requireOnCode"} {
		raw, ok := opt[key]
		if !ok {
			continue
		}
		s, ok := raw.(string)
		if !ok {
			continue
		}
		s = strings.TrimSpace(s)
		if s == "" {
			delete(opt, key)
			continue
		}
		n := normalizeFormDepValue(s, lookup)
		if strings.TrimSpace(n) == "" {
			delete(opt, key)
			continue
		}
		opt[key] = n
	}
}

func formOptionMatchKey(groupCode, fieldName, typ string, displayOrder int64) string {
	return strings.ToLower(strings.TrimSpace(groupCode)) + "\x00" +
		strings.ToLower(strings.TrimSpace(fieldName)) + "\x00" +
		strings.ToLower(strings.TrimSpace(typ)) + "\x00" +
		strconv.FormatInt(displayOrder, 10)
}

func indexFormOptionsByMatchKey(form map[string]interface{}) map[string]map[string]interface{} {
	out := map[string]map[string]interface{}{}
	walkFormOptions(form, func(gc string, opt map[string]interface{}) {
		fn := stringFromAny(opt["fieldName"])
		typ := stringFromAny(opt["type"])
		do := intFromAny(opt["displayOrder"])
		k := formOptionMatchKey(gc, fn, typ, do)
		out[k] = opt
	})
	return out
}

func remapConfigFieldRefs(cfg interface{}, remap map[string]string) {
	if cfg == nil || len(remap) == 0 {
		return
	}
	switch v := cfg.(type) {
	case map[string]interface{}:
		for k, val := range v {
			switch tv := val.(type) {
			case string:
				if nv, ok := remap[tv]; ok && nv != "" {
					v[k] = nv
				}
			default:
				remapConfigFieldRefs(val, remap)
			}
		}
	case []interface{}:
		for i, el := range v {
			switch te := el.(type) {
			case string:
				if nv, ok := remap[te]; ok && nv != "" {
					v[i] = nv
				}
			default:
				remapConfigFieldRefs(el, remap)
			}
		}
	}
}

// formConfigKeyReferencesOtherField reports config keys whose string values are sibling form field codes.
func formConfigKeyReferencesOtherField(k string) bool {
	if k == "group" {
		return true
	}
	if strings.HasSuffix(k, "Field") && !strings.HasSuffix(k, "FieldType") {
		return true
	}
	return false
}

func collectFormOptionCodesLower(form map[string]interface{}) map[string]struct{} {
	out := map[string]struct{}{}
	walkFormOptions(form, func(_ string, opt map[string]interface{}) {
		c := strings.TrimSpace(stringFromAny(opt["code"]))
		if c != "" {
			out[strings.ToLower(c)] = struct{}{}
		}
	})
	return out
}

// sanitizeDanglingFormConfigRefs removes config entries that still point at field codes not present on this form
// (e.g. plan.poolField left as a source UUID after the resourcePool field was dropped from the export).
func sanitizeDanglingFormConfigRefs(cfg interface{}, validLower map[string]struct{}) {
	switch v := cfg.(type) {
	case map[string]interface{}:
		var drop []string
		for k, val := range v {
			s, ok := val.(string)
			if ok {
				s = strings.TrimSpace(s)
				if s != "" && formConfigKeyReferencesOtherField(k) {
					if _, ok := validLower[strings.ToLower(s)]; !ok {
						drop = append(drop, k)
						continue
					}
				}
			}
			sanitizeDanglingFormConfigRefs(val, validLower)
		}
		for _, k := range drop {
			delete(v, k)
		}
	case []interface{}:
		for _, el := range v {
			sanitizeDanglingFormConfigRefs(el, validLower)
		}
	}
}

// formConfigFieldTypePrefixes are paired with "<prefix>Field" and "<prefix>FieldType" in Morpheus option configs.
var formConfigFieldTypePrefixes = []string{
	"group", "cloud", "pool", "layout", "plan", "disk", "resourcePool", "instanceType",
}

// removeOrphanFieldTypeKeys deletes *FieldType when *Field is missing or empty (e.g. poolField removed but poolFieldType still "field").
func removeOrphanFieldTypeKeys(cfg map[string]interface{}) {
	for _, prefix := range formConfigFieldTypePrefixes {
		typeKey := prefix + "FieldType"
		fieldKey := prefix + "Field"
		if _, hasType := cfg[typeKey]; !hasType {
			continue
		}
		fv, hasField := cfg[fieldKey]
		if !hasField {
			delete(cfg, typeKey)
			continue
		}
		fs, ok := fv.(string)
		if !ok || strings.TrimSpace(fs) == "" {
			delete(cfg, typeKey)
		}
	}
}

// stripEmptyStringsFromOptionConfig removes "" string values from option config maps so the API does not get inconsistent empty refs.
func stripEmptyStringsFromOptionConfig(cfg interface{}) {
	switch v := cfg.(type) {
	case map[string]interface{}:
		var drop []string
		for k, val := range v {
			switch t := val.(type) {
			case string:
				if strings.TrimSpace(t) == "" {
					drop = append(drop, k)
				}
			case map[string]interface{}:
				stripEmptyStringsFromOptionConfig(t)
			case []interface{}:
				for _, el := range t {
					stripEmptyStringsFromOptionConfig(el)
				}
			}
		}
		for _, k := range drop {
			delete(v, k)
		}
	case []interface{}:
		for _, el := range v {
			stripEmptyStringsFromOptionConfig(el)
		}
	}
}

func normalizeFormOptionConfigMaps(form map[string]interface{}) {
	walkFormOptions(form, func(_ string, opt map[string]interface{}) {
		cfg, ok := opt["config"].(map[string]interface{})
		if !ok || cfg == nil {
			return
		}
		stripEmptyStringsFromOptionConfig(cfg)
		removeOrphanFieldTypeKeys(cfg)
		removeIncompleteLayoutFieldRefs(cfg)
	})
}

// removeIncompleteLayoutFieldRefs drops layout *Field refs when layoutId is missing; Morpheus often rejects plan/layoutFieldType:field without a layout id.
func removeIncompleteLayoutFieldRefs(cfg map[string]interface{}) {
	if strings.TrimSpace(stringFromAny(cfg["layoutFieldType"])) != "field" {
		return
	}
	if strings.TrimSpace(stringFromAny(cfg["layoutId"])) != "" {
		return
	}
	delete(cfg, "layoutField")
	delete(cfg, "layoutFieldType")
	delete(cfg, "layoutId")
}

// rewriteUUIDFieldGroupCodesToSlugs replaces UUID fieldGroup codes on create so the destination API accepts new groups.
func rewriteUUIDFieldGroupCodesToSlugs(form map[string]interface{}) {
	fgs, ok := form["fieldGroups"].([]interface{})
	if !ok {
		return
	}
	for i, g := range fgs {
		gm, ok := g.(map[string]interface{})
		if !ok {
			continue
		}
		c := strings.TrimSpace(stringFromAny(gm["code"]))
		if isLikelyUUID(c) {
			gm["code"] = fmt.Sprintf("fieldgroup%d", i)
		}
	}
}

// dedupeFormOptionFieldNames ensures each option has a distinct fieldName (customOptions key). Duplicate names (e.g. two fields both "diskThree") cause Morpheus 500s.
func dedupeFormOptionFieldNames(metas []formOptMeta) {
	used := map[string]struct{}{}
	for i := range metas {
		m := &metas[i]
		opt := m.opt
		fn := strings.TrimSpace(stringFromAny(opt["fieldName"]))
		if fn == "" {
			fn = strings.TrimSpace(m.finalCode)
		}
		if fn == "" {
			continue
		}
		if _, dup := used[strings.ToLower(fn)]; dup {
			fn = strings.TrimSpace(m.finalCode)
			opt["fieldName"] = fn
		} else if strings.TrimSpace(stringFromAny(opt["fieldName"])) == "" {
			opt["fieldName"] = fn
		}
		if fn == "" {
			continue
		}
		used[strings.ToLower(fn)] = struct{}{}
		m.fieldName = fn
	}
}

func buildOptionTypeFormPayload(src, dst *morpheus.Client, form map[string]interface{}, destForm map[string]interface{}) ([]byte, error) {
	var clone map[string]interface{}
	raw, err := json.Marshal(form)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &clone); err != nil {
		return nil, err
	}

	for _, k := range []string{"id", "dateCreated", "lastUpdated", "account", "accountId", "uuid", "owner", "stats"} {
		delete(clone, k)
	}

	// Keep destination identity on update — source name/code can collide with another form's name.
	if destForm != nil {
		if dn := strings.TrimSpace(stringFromAny(destForm["name"])); dn != "" {
			clone["name"] = dn
		}
		if dc := strings.TrimSpace(stringFromAny(destForm["code"])); dc != "" {
			clone["code"] = dc
		}
	}

	if destForm == nil {
		rewriteUUIDFieldGroupCodesToSlugs(clone)
	}

	destIdx := map[string]map[string]interface{}{}
	if destForm != nil {
		destIdx = indexFormOptionsByMatchKey(destForm)
	}

	usedCodes := map[string]struct{}{}
	if destForm != nil {
		walkFormOptions(destForm, func(_ string, opt map[string]interface{}) {
			c := strings.TrimSpace(stringFromAny(opt["code"]))
			if c != "" {
				usedCodes[strings.ToLower(c)] = struct{}{}
			}
		})
	}

	var metas []formOptMeta
	walkFormOptions(clone, func(gc string, opt map[string]interface{}) {
		if gc != "" && isFormLibraryInputIDRef(opt) {
			return
		}
		metas = append(metas, formOptMeta{
			opt:          opt,
			groupCode:    gc,
			oldCode:      strings.TrimSpace(stringFromAny(opt["code"])),
			fieldName:    strings.TrimSpace(stringFromAny(opt["fieldName"])),
			typ:          strings.TrimSpace(stringFromAny(opt["type"])),
			displayOrder: intFromAny(opt["displayOrder"]),
		})
	})

	remap := map[string]string{}
	for i := range metas {
		m := &metas[i]
		matchKey := formOptionMatchKey(m.groupCode, m.fieldName, m.typ, m.displayOrder)
		if destOpt, ok := destIdx[matchKey]; ok {
			fc := strings.TrimSpace(stringFromAny(destOpt["code"]))
			if fc == "" {
				fc = uniqueFormFieldCode(formFieldCodeSlug(m.fieldName), usedCodes)
			}
			m.finalCode = fc
			if m.oldCode != "" && m.oldCode != m.finalCode {
				remap[m.oldCode] = m.finalCode
			}
			continue
		}
		if m.oldCode != "" && !isLikelyUUID(m.oldCode) {
			m.finalCode = uniqueFormFieldCode(m.oldCode, usedCodes)
		} else {
			m.finalCode = uniqueFormFieldCode(formFieldCodeSlug(m.fieldName), usedCodes)
		}
		if m.oldCode != "" && m.oldCode != m.finalCode {
			remap[m.oldCode] = m.finalCode
		}
	}

	var listErr error
	for i := range metas {
		if listErr != nil {
			break
		}
		m := &metas[i]
		opt := m.opt
		matchKey := formOptionMatchKey(m.groupCode, m.fieldName, m.typ, m.displayOrder)
		if destOpt, ok := destIdx[matchKey]; ok {
			if id := intFromAny(destOpt["id"]); id > 0 {
				opt["id"] = id
			}
			opt["code"] = m.finalCode
		} else {
			delete(opt, "id")
			opt["code"] = m.finalCode
		}

		oldName := strings.TrimSpace(stringFromAny(opt["name"]))
		if oldName != "" && m.oldCode != "" && strings.EqualFold(oldName, m.oldCode) && isLikelyUUID(m.oldCode) {
			if lbl := strings.TrimSpace(stringFromAny(opt["fieldLabel"])); lbl != "" {
				opt["name"] = lbl
			} else if m.fieldName != "" {
				opt["name"] = m.fieldName
			} else {
				opt["name"] = m.finalCode
			}
		}

		for _, fk := range []string{"uuid", "owner", "stats", "account", "accountId"} {
			delete(opt, fk)
		}

		if err := ensureOptionTypeListDependency(src, dst, opt); err != nil {
			listErr = err
			break
		}
		if cfg, ok := opt["config"].(map[string]interface{}); ok && len(remap) > 0 {
			remapConfigFieldRefs(cfg, remap)
		}
	}
	if listErr != nil {
		return nil, listErr
	}

	dedupeFormOptionFieldNames(metas)

	validCodes := collectFormOptionCodesLower(clone)
	for i := range metas {
		if cfg, ok := metas[i].opt["config"].(map[string]interface{}); ok {
			sanitizeDanglingFormConfigRefs(cfg, validCodes)
		}
	}
	normalizeFormOptionConfigMaps(clone)

	lookup := buildFormDepLookup(metas)
	for i := range metas {
		normalizeFormFieldDependencies(metas[i].opt, lookup)
	}

	stripped := stripJSONNulls(clone)
	root, ok := stripped.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("form payload strip nulls: unexpected type %T", stripped)
	}

	return json.Marshal(map[string]interface{}{"optionTypeForm": root})
}

// findOptionTypeFormID resolves an existing destination form by exact name and/or code.
// When name is provided, only an exact name match counts — code alone must not match a
// different form (Morpheus names are globally unique; code can be reused across forms).
func findOptionTypeFormID(dst *morpheus.Client, code, name string) int64 {
	wantCode := strings.ToLower(strings.TrimSpace(code))
	wantName := strings.ToLower(strings.TrimSpace(name))
	if wantCode == "" && wantName == "" {
		return 0
	}
	if wantName != "" {
		return findOptionTypeFormIDByName(dst, name)
	}
	return findOptionTypeFormIDByCode(dst, code)
}

func listDestinationOptionTypeForms(dst *morpheus.Client) ([]map[string]interface{}, error) {
	raws, err := paginateList(dst, "/api/library/option-type-forms", "optionTypeForms")
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]interface{}, 0, len(raws))
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		if intFromAny(row["id"]) <= 0 {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func findOptionTypeFormIDByCode(dst *morpheus.Client, code string) int64 {
	wantCode := strings.ToLower(strings.TrimSpace(code))
	if wantCode == "" {
		return 0
	}
	rows, err := listDestinationOptionTypeForms(dst)
	if err != nil {
		return 0
	}
	for _, row := range rows {
		if strings.ToLower(strings.TrimSpace(stringFromAny(row["code"]))) == wantCode {
			return intFromAny(row["id"])
		}
	}
	return 0
}

func findOptionTypeFormIDByName(dst *morpheus.Client, name string) int64 {
	wantName := strings.ToLower(strings.TrimSpace(name))
	if wantName == "" {
		return 0
	}
	rows, err := listDestinationOptionTypeForms(dst)
	if err != nil {
		return 0
	}
	for _, row := range rows {
		if strings.ToLower(strings.TrimSpace(stringFromAny(row["name"]))) == wantName {
			return intFromAny(row["id"])
		}
	}
	return 0
}

var formCompareKeys = []string{
	"name", "code", "description", "labels", "visibility", "fieldGroups", "options", "formType",
}

func formNeedsUpdate(src, dst *morpheus.Client, srcForm map[string]interface{}, destID int64) bool {
	if destID <= 0 || srcForm == nil {
		return true
	}
	destForm, err := loadDestinationOptionTypeForm(dst, destID)
	if err != nil || destForm == nil {
		return true
	}
	srcName := strings.ToLower(strings.TrimSpace(stringFromAny(srcForm["name"])))
	if srcName != "" {
		destName := strings.ToLower(strings.TrimSpace(stringFromAny(destForm["name"])))
		if srcName != destName {
			return true
		}
	}
	payload, err := buildOptionTypeFormPayload(src, dst, srcForm, destForm)
	if err != nil {
		return true
	}
	var wrap map[string]json.RawMessage
	if json.Unmarshal(payload, &wrap) != nil {
		return true
	}
	var expected map[string]interface{}
	if json.Unmarshal(wrap["optionTypeForm"], &expected) != nil {
		return true
	}
	return !formWritableContentEqual(expected, destForm)
}

func formWritableContentEqual(a, b map[string]interface{}) bool {
	na := stripFormCompareMetadata(a)
	nb := stripFormCompareMetadata(b)
	if na == nil || nb == nil {
		return false
	}
	for _, k := range formCompareKeys {
		if !jsonEqualNormalized(na[k], nb[k]) {
			return false
		}
	}
	return true
}

func stripFormCompareMetadata(form map[string]interface{}) map[string]interface{} {
	raw, err := json.Marshal(form)
	if err != nil {
		return nil
	}
	var clone map[string]interface{}
	if json.Unmarshal(raw, &clone) != nil {
		return nil
	}
	for _, k := range []string{"id", "dateCreated", "lastUpdated", "account", "accountId", "uuid", "owner", "stats"} {
		delete(clone, k)
	}
	normalizeFormOptionConfigMaps(clone)
	return clone
}

func putOptionTypeForm(dst, src *morpheus.Client, obj map[string]interface{}, formID int64, formCode, name string) (outcome, msg string, err error) {
	destForm, err := loadDestinationOptionTypeForm(dst, formID)
	if err != nil {
		return "", "", fmt.Errorf("load destination form: %v", err)
	}
	payload, err := buildOptionTypeFormPayload(src, dst, obj, destForm)
	if err != nil {
		return "", "", fmt.Errorf("prepare form: %v", err)
	}
	_, err = dst.PutRaw(fmt.Sprintf("/api/library/option-type-forms/%d", formID), payload)
	if err != nil && isDuplicateErr(err) {
		if altID := findOptionTypeFormIDByName(dst, name); altID > 0 && altID != formID {
			return putOptionTypeForm(dst, src, obj, altID, formCode, name)
		}
	}
	if err != nil {
		return "", "", fmt.Errorf("update form: %v", err)
	}
	return "updated", "Updated form on destination", nil
}

func loadDestinationOptionTypeForm(dst *morpheus.Client, id int64) (map[string]interface{}, error) {
	if id <= 0 {
		return nil, nil
	}
	body, err := dst.GetRaw(fmt.Sprintf("/api/library/option-type-forms/%d", id))
	if err != nil {
		return nil, err
	}
	var wrap map[string]json.RawMessage
	if json.Unmarshal(body, &wrap) != nil {
		return nil, nil
	}
	rawForm, ok := wrap["optionTypeForm"]
	if !ok {
		return nil, nil
	}
	var dm map[string]interface{}
	if json.Unmarshal(rawForm, &dm) != nil || dm == nil {
		return nil, nil
	}
	return dm, nil
}

func parseOptionTypeFormIDFromResponse(body []byte) int64 {
	var root map[string]interface{}
	if json.Unmarshal(body, &root) == nil {
		if id := intFromAny(root["id"]); id > 0 {
			return id
		}
	}
	var wrap map[string]json.RawMessage
	if json.Unmarshal(body, &wrap) != nil {
		return 0
	}
	raw, ok := wrap["optionTypeForm"]
	if !ok {
		return 0
	}
	var row map[string]interface{}
	if json.Unmarshal(raw, &row) != nil {
		return 0
	}
	return intFromAny(row["id"])
}

func buildOptionTypePayload(src, dst *morpheus.Client, obj map[string]interface{}, code string) ([]byte, error) {
	var clone map[string]interface{}
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &clone); err != nil {
		return nil, err
	}
	for _, k := range []string{
		"id", "dateCreated", "lastUpdated", "account", "accountId",
		"uuid", "owner", "stats",
	} {
		delete(clone, k)
	}
	normalizeOptionTypeForCreate(clone, code)
	if err := ensureOptionTypeListDependency(src, dst, clone); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]interface{}{"optionType": clone})
}

func createOptionTypeOnDestination(src, dst *morpheus.Client, obj map[string]interface{}, code string) (int64, error) {
	payload, err := buildOptionTypePayload(src, dst, obj, code)
	if err != nil {
		return 0, err
	}

	wrapperKeys := []string{"optionType"}
	endpoints := []string{"/api/library/option-types"}

	var lastErr error
	for _, ep := range endpoints {
		for _, wkey := range wrapperKeys {
			_ = wkey // fixed wrapper (optionType), loop kept to minimize code churn
			body, err := dst.PostRaw(ep, payload)
			if err != nil {
				lastErr = err
				continue
			}
			if id := parseOptionTypeIDFromResponse(body); id > 0 {
				return id, nil
			}
			// POST may succeed but return a shape we do not parse — resolve by code on destination.
			if id := findOptionTypeIDByCode(dst, code); id > 0 {
				return id, nil
			}
			lastErr = fmt.Errorf("response missing option type id and lookup by code %q failed", code)
		}
	}
	if id := findOptionTypeIDByCode(dst, code); id > 0 {
		return id, nil
	}
	if lastErr != nil {
		return 0, lastErr
	}
	return 0, fmt.Errorf("could not create option type (POST failed on known endpoints)")
}

func normalizeOptionTypeForCreate(optionType map[string]interface{}, fallbackCode string) {
	code := strings.TrimSpace(stringFromAny(optionType["code"]))
	if code == "" {
		code = strings.TrimSpace(fallbackCode)
	}
	if code == "" {
		code = strings.TrimSpace(stringFromAny(optionType["fieldName"]))
	}

	name := strings.TrimSpace(stringFromAny(optionType["name"]))
	if name == "" {
		name = code
	}
	fieldName := strings.TrimSpace(stringFromAny(optionType["fieldName"]))
	if fieldName == "" {
		fieldName = code
	}
	fieldLabel := strings.TrimSpace(stringFromAny(optionType["fieldLabel"]))
	if fieldLabel == "" {
		if name != "" {
			fieldLabel = name
		} else {
			fieldLabel = fieldName
		}
	}

	if code != "" {
		optionType["code"] = code
	}
	if name != "" {
		optionType["name"] = name
	}
	if fieldName != "" {
		optionType["fieldName"] = fieldName
	}
	if fieldLabel != "" {
		optionType["fieldLabel"] = fieldLabel
	}

	// Keep list-backed inputs consistent for create payloads.
	if ol, ok := optionType["optionList"].(map[string]interface{}); ok && ol != nil {
		if intFromAny(ol["id"]) > 0 || strings.TrimSpace(stringFromAny(ol["name"])) != "" {
			if strings.TrimSpace(stringFromAny(optionType["optionSource"])) == "" {
				optionType["optionSource"] = "list"
			}
		}
	}
}

func parseOptionTypeIDFromResponse(body []byte) int64 {
	var root map[string]interface{}
	if json.Unmarshal(body, &root) == nil {
		if id := intFromAny(root["id"]); id > 0 {
			return id
		}
	}
	var wrap map[string]json.RawMessage
	if json.Unmarshal(body, &wrap) != nil {
		return 0
	}
	for _, key := range []string{"optionType", "option"} {
		raw, ok := wrap[key]
		if !ok {
			continue
		}
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		if id := intFromAny(row["id"]); id > 0 {
			return id
		}
	}
	return 0
}

func findOptionTypeIDByCode(dst *morpheus.Client, code string) int64 {
	code = strings.TrimSpace(code)
	if code == "" {
		return 0
	}
	want := strings.ToLower(code)
	paths := []string{
		fmt.Sprintf("/api/library/option-types?phrase=%s&max=100&offset=0", url.QueryEscape(code)),
		fmt.Sprintf("/api/options/types?phrase=%s&max=100&offset=0", url.QueryEscape(code)),
	}
	for _, path := range paths {
		body, err := dst.GetRaw(path)
		if err != nil {
			continue
		}
		var wrapper map[string]json.RawMessage
		if json.Unmarshal(body, &wrapper) != nil {
			continue
		}
		raw, ok := wrapper["optionTypes"]
		if !ok {
			continue
		}
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			continue
		}
		for _, it := range items {
			var row map[string]interface{}
			if json.Unmarshal(it, &row) != nil {
				continue
			}
			c := strings.TrimSpace(stringFromAny(row["code"]))
			if strings.ToLower(c) != want {
				continue
			}
			if id := intFromAny(row["id"]); id > 0 {
				return id
			}
		}
	}
	return 0
}

func migrateTaskWithAutomation(src, dst *morpheus.Client, item SelectedItem, state *automationState) ItemResult {
	name := strings.TrimSpace(item.Name)
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(item.RawJSON), &obj); err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "error", Message: fmt.Sprintf("invalid task json: %v", err)}
	}
	if n := stringFromAny(obj["name"]); n != "" {
		name = n
	}

	if err := resolveTaskIntegrations(src, dst, obj); err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "blocked", Message: err.Error()}
	}

	wantRepoID, repoBacked := repositoryBindingFromTask(obj)

	stripTaskForWrite(obj)

	payload, err := json.Marshal(map[string]interface{}{"task": obj})
	if err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "error", Message: fmt.Sprintf("marshal: %v", err)}
	}

	if err := state.refreshDestTasks(dst); err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "error", Message: fmt.Sprintf("list destination tasks: %v", err)}
	}

	existingID := state.destTaskID(name)
	if existingID > 0 {
		if taskNeedsUpdate(obj, dst, existingID) {
			_, err = dst.PutRaw(fmt.Sprintf("/api/tasks/%d", existingID), payload)
			if err != nil {
				return ItemResult{Name: name, Type: item.Type, Status: "error", Message: fmt.Sprintf("update task: %v", err)}
			}
			if msg := verifyRepositoryTaskBinding(dst, existingID, wantRepoID, repoBacked); msg != "" {
				return ItemResult{Name: name, Type: item.Type, Status: "partial", Message: msg}
			}
			return ItemResult{Name: name, Type: item.Type, Status: "success", Outcome: "updated", Message: "Updated existing task on destination"}
		}
		return ItemResult{Name: name, Type: item.Type, Status: "skipped", Message: "Task already exists and matches source"}
	}

	_, err = dst.PostRaw("/api/tasks", payload)
	if err != nil {
		if isDuplicateErr(err) {
			if err2 := state.refreshDestTasks(dst); err2 != nil {
				return ItemResult{Name: name, Type: item.Type, Status: "error", Message: err2.Error()}
			}
			if id := state.destTaskID(name); id > 0 {
				if taskNeedsUpdate(obj, dst, id) {
					_, err = dst.PutRaw(fmt.Sprintf("/api/tasks/%d", id), payload)
					if err != nil {
						return ItemResult{Name: name, Type: item.Type, Status: "error", Message: fmt.Sprintf("update after duplicate: %v", err)}
					}
					if msg := verifyRepositoryTaskBinding(dst, id, wantRepoID, repoBacked); msg != "" {
						return ItemResult{Name: name, Type: item.Type, Status: "partial", Message: msg}
					}
					return ItemResult{Name: name, Type: item.Type, Status: "success", Outcome: "updated", Message: "Updated existing task on destination"}
				}
				return ItemResult{Name: name, Type: item.Type, Status: "skipped", Message: "Task already exists on destination"}
			}
		}
		return ItemResult{Name: name, Type: item.Type, Status: "error", Message: err.Error()}
	}

	if err := state.refreshDestTasks(dst); err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "success", Outcome: "created", Message: fmt.Sprintf("Created task (could not refresh task list: %v)", err)}
	}
	if repoBacked {
		if msg := verifyAndRollbackNewRepositoryTask(dst, name, wantRepoID, state); msg != "" {
			return ItemResult{Name: name, Type: item.Type, Status: "partial", Message: msg}
		}
	}
	return ItemResult{Name: name, Type: item.Type, Status: "success", Outcome: "created", Message: "Created task on destination"}
}

// repositoryBindingFromTask returns the destination Git integration id and true when the task is repository-backed.
func repositoryBindingFromTask(task map[string]interface{}) (wantRepoID int64, repoBacked bool) {
	file, ok := task["file"].(map[string]interface{})
	if !ok || !strings.EqualFold(stringFromAny(file["sourceType"]), "repository") {
		return 0, false
	}
	repo, _ := file["repository"].(map[string]interface{})
	return intFromAny(repo["id"]), true
}

func parseTaskGETRepositoryBinding(body []byte) (sourceType string, repoID int64, ok bool) {
	var wrap map[string]json.RawMessage
	if json.Unmarshal(body, &wrap) != nil {
		return "", 0, false
	}
	raw, has := wrap["task"]
	if !has {
		return "", 0, false
	}
	var row map[string]interface{}
	if json.Unmarshal(raw, &row) != nil {
		return "", 0, false
	}
	file, hasFile := row["file"].(map[string]interface{})
	if !hasFile {
		return "", 0, false
	}
	st := stringFromAny(file["sourceType"])
	repo, rok := file["repository"].(map[string]interface{})
	if !rok || repo == nil {
		return st, 0, true
	}
	return st, intFromAny(repo["id"]), true
}

func assertDestTaskRepositoryBindingByID(dst *morpheus.Client, taskID int64, wantRepoID int64) error {
	if taskID <= 0 {
		return fmt.Errorf("invalid task id")
	}
	if wantRepoID <= 0 {
		return fmt.Errorf("expected a positive Git integration id for the repository link")
	}
	body, err := dst.GetRaw(fmt.Sprintf("/api/tasks/%d", taskID))
	if err != nil {
		return fmt.Errorf("could not read task after save: %v", err)
	}
	st, rid, ok := parseTaskGETRepositoryBinding(body)
	if !ok {
		return fmt.Errorf("could not parse destination task response")
	}
	if !strings.EqualFold(st, "repository") {
		return fmt.Errorf("destination file is not repository-backed (sourceType=%q)", st)
	}
	if rid != wantRepoID {
		return fmt.Errorf("destination task points at Git integration id %d, but migration expected id %d (no matching integration, wrong integration, or missing SSH key on the destination)", rid, wantRepoID)
	}
	return nil
}

func verifyAndRollbackNewRepositoryTask(dst *morpheus.Client, taskName string, wantRepoID int64, state *automationState) string {
	if err := state.refreshDestTasks(dst); err != nil {
		return fmt.Sprintf("PARTIAL MIGRATION: could not verify the Git repository link (%v). Inspect the task on the destination.", err)
	}
	tid := state.destTaskID(taskName)
	if tid <= 0 {
		return "PARTIAL MIGRATION: the task was not found after create when verifying the repository link. Check permissions and duplicate names."
	}
	bindErr := assertDestTaskRepositoryBindingByID(dst, tid, wantRepoID)
	if bindErr == nil {
		return ""
	}
	delErr := dst.DeleteRaw(fmt.Sprintf("/api/tasks/%d", tid))
	if delErr != nil {
		return fmt.Sprintf("PARTIAL MIGRATION: %v Remove or repair task id %d on the destination manually (automatic delete failed: %v).", bindErr, tid, delErr)
	}
	_ = state.refreshDestTasks(dst)
	return fmt.Sprintf("PARTIAL MIGRATION: %v The incomplete task was deleted on the destination. Add the Git integration and SSH key used on the source (see earlier messages for the source key name), then re-run.", bindErr)
}

func verifyRepositoryTaskBinding(dst *morpheus.Client, taskID int64, wantRepoID int64, repoBacked bool) string {
	if !repoBacked || wantRepoID <= 0 {
		return ""
	}
	if err := assertDestTaskRepositoryBindingByID(dst, taskID, wantRepoID); err != nil {
		return fmt.Sprintf("PARTIAL MIGRATION: %v Fix the Git integration or SSH key pair on the destination and migrate again.", err)
	}
	return ""
}

func taskNeedsUpdate(srcPayload map[string]interface{}, dst *morpheus.Client, destID int64) bool {
	body, err := dst.GetRaw(fmt.Sprintf("/api/tasks/%d", destID))
	if err != nil {
		return true
	}
	var wrap map[string]json.RawMessage
	if json.Unmarshal(body, &wrap) != nil {
		return true
	}
	raw, ok := wrap["task"]
	if !ok {
		return true
	}
	var dest map[string]interface{}
	if json.Unmarshal(raw, &dest) != nil {
		return true
	}
	return !taskContentEqual(srcPayload, dest)
}

func taskContentEqual(src, dst map[string]interface{}) bool {
	srcType := taskTypeCode(src)
	dstType := taskTypeCode(dst)
	if srcType != dstType {
		return false
	}
	if !mapsEqualShallow(src["taskOptions"], dst["taskOptions"]) {
		return false
	}
	if !filePartEqual(src["file"], dst["file"]) {
		return false
	}
	for _, k := range []string{"executeTarget", "visibility", "resultType", "continueOnError", "retryable", "retryCount", "retryDelaySeconds", "allowCustomConfig", "labels"} {
		if !jsonEqualNormalized(src[k], dst[k]) {
			return false
		}
	}
	return true
}

func taskTypeCode(t map[string]interface{}) string {
	tm, ok := t["taskType"].(map[string]interface{})
	if !ok {
		return ""
	}
	return stringFromAny(tm["code"])
}

func filePartEqual(a, b interface{}) bool {
	am, aok := a.(map[string]interface{})
	bm, bok := b.(map[string]interface{})
	if !aok && !bok {
		return true
	}
	if !aok || !bok {
		return false
	}
	st := stringFromAny(am["sourceType"])
	if st == "" {
		st = "local"
	}
	stb := stringFromAny(bm["sourceType"])
	if stb == "" {
		stb = "local"
	}
	if st != stb {
		return false
	}
	if st == "local" {
		return strings.ReplaceAll(stringFromAny(am["content"]), "\r\n", "\n") ==
			strings.ReplaceAll(stringFromAny(bm["content"]), "\r\n", "\n")
	}
	// repository
	ra, _ := am["repository"].(map[string]interface{})
	rb, _ := bm["repository"].(map[string]interface{})
	return intFromAny(ra["id"]) == intFromAny(rb["id"]) &&
		stringFromAny(am["contentPath"]) == stringFromAny(bm["contentPath"]) &&
		stringFromAny(am["contentRef"]) == stringFromAny(bm["contentRef"])
}

func mapsEqualShallow(a, b interface{}) bool {
	am, aok := a.(map[string]interface{})
	bm, bok := b.(map[string]interface{})
	if !aok && !bok {
		return true
	}
	if !aok || !bok {
		return false
	}
	keys := map[string]struct{}{}
	for k := range am {
		keys[k] = struct{}{}
	}
	for k := range bm {
		keys[k] = struct{}{}
	}
	for k := range keys {
		va, oka := am[k]
		vb, okb := bm[k]
		if !oka {
			va = nil
		}
		if !okb {
			vb = nil
		}
		if va == nil && vb == nil {
			continue
		}
		if !jsonEqualNormalized(va, vb) {
			return false
		}
	}
	return true
}

func jsonEqualNormalized(a, b interface{}) bool {
	ja, e1 := json.Marshal(a)
	jb, e2 := json.Marshal(b)
	if e1 != nil || e2 != nil {
		return false
	}
	return string(ja) == string(jb)
}

func stripTaskForWrite(obj map[string]interface{}) {
	for _, k := range []string{"id", "dateCreated", "lastUpdated", "account", "accountId"} {
		delete(obj, k)
	}
	if file, ok := obj["file"].(map[string]interface{}); ok {
		delete(file, "id")
	}
}

func migrateWorkflowWithAutomation(src, dst *morpheus.Client, item SelectedItem, state *automationState) ItemResult {
	name := strings.TrimSpace(item.Name)
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(item.RawJSON), &obj); err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "error", Message: fmt.Sprintf("invalid workflow json: %v", err)}
	}
	if n := stringFromAny(obj["name"]); n != "" {
		name = n
	}
	wfType := stringFromAny(obj["type"])
	if wfType == "" {
		wfType = "operation"
	}

	if err := state.refreshDestTasks(dst); err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "error", Message: fmt.Sprintf("list destination tasks: %v", err)}
	}

	if dep := ensureNestedWorkflowTasks(src, dst, obj, name, state); dep != nil {
		return *dep
	}

	if dep := ensureMissingOptionTypes(src, dst, obj, name, state); dep != nil {
		return *dep
	}

	tasksPayload, err := buildWorkflowTasksPayload(obj, state.destTaskMapCopy())
	if err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "blocked", Message: err.Error()}
	}

	state.reloadDestOptionTypes(dst)
	optIDs, optWarn := mapWorkflowOptionTypes(obj["optionTypes"], state.destOptionTypeMapCopy())

	out := map[string]interface{}{
		"type":              wfType,
		"visibility":        obj["visibility"],
		"name":              name,
		"description":       obj["description"],
		"labels":            obj["labels"],
		"platform":          obj["platform"],
		"allowCustomConfig": obj["allowCustomConfig"],
		"tasks":             tasksPayload,
		"optionTypes":       optIDs,
	}
	// drop null keys Morpheus may reject
	for k, v := range out {
		if v == nil {
			delete(out, k)
		}
	}

	stripEmptyAllowCustom(out)

	payload, err := json.Marshal(map[string]interface{}{"taskSet": out})
	if err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "error", Message: fmt.Sprintf("marshal: %v", err)}
	}

	msgExtra := ""
	if optWarn != "" {
		msgExtra = "; " + optWarn
	}
	if loadErr := state.optionTypesLoadError(); loadErr != "" && len(optIDs) == 0 && obj["optionTypes"] != nil {
		if arr, ok := obj["optionTypes"].([]interface{}); ok && len(arr) > 0 {
			msgExtra = msgExtra + "; " + loadErr
		}
	}

	if err := state.refreshDestWorkflows(dst); err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "error", Message: fmt.Sprintf("list workflows: %v", err)}
	}
	existingID := state.destWorkflowID(name, wfType)

	if existingID > 0 {
		_, err = dst.PutRaw(fmt.Sprintf("/api/task-sets/%d", existingID), payload)
		if err != nil {
			return ItemResult{Name: name, Type: item.Type, Status: "error", Message: fmt.Sprintf("update workflow: %v", err)}
		}
		return ItemResult{Name: name, Type: item.Type, Status: "success", Outcome: "updated", Message: "Updated workflow on destination" + msgExtra}
	}

	_, err = dst.PostRaw("/api/task-sets", payload)
	if err != nil {
		if isDuplicateErr(err) {
			if err2 := state.refreshDestWorkflows(dst); err2 == nil {
				if id := state.destWorkflowID(name, wfType); id > 0 {
					_, err = dst.PutRaw(fmt.Sprintf("/api/task-sets/%d", id), payload)
					if err != nil {
						return ItemResult{Name: name, Type: item.Type, Status: "error", Message: fmt.Sprintf("update after duplicate: %v", err)}
					}
					return ItemResult{Name: name, Type: item.Type, Status: "success", Outcome: "updated", Message: "Updated workflow on destination" + msgExtra}
				}
			}
			return ItemResult{Name: name, Type: item.Type, Status: "skipped", Message: "Workflow may already exist; could not resolve for update" + msgExtra}
		}
		return ItemResult{Name: name, Type: item.Type, Status: "error", Message: err.Error() + msgExtra}
	}

	if err := state.refreshDestWorkflows(dst); err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "success", Outcome: "created", Message: "Created workflow" + msgExtra}
	}
	return ItemResult{Name: name, Type: item.Type, Status: "success", Outcome: "created", Message: "Created workflow on destination" + msgExtra}
}

func stripEmptyAllowCustom(m map[string]interface{}) {
	if ac, ok := m["allowCustomConfig"]; ok && ac == nil {
		delete(m, "allowCustomConfig")
	}
}

func buildWorkflowTasksPayload(wf map[string]interface{}, destNameToID map[string]int64) ([]map[string]interface{}, error) {
	tst, ok := wf["taskSetTasks"].([]interface{})
	if !ok || len(tst) == 0 {
		return []map[string]interface{}{}, nil
	}

	type row struct {
		order int64
		phase string
		name  string
	}
	var rows []row
	for _, e := range tst {
		em, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		tm, ok := em["task"].(map[string]interface{})
		if !ok {
			continue
		}
		tn := strings.TrimSpace(stringFromAny(tm["name"]))
		if tn == "" {
			continue
		}
		rows = append(rows, row{
			order: intFromAny(em["taskOrder"]),
			phase: stringFromAny(em["taskPhase"]),
			name:  tn,
		})
	}
	if len(rows) == 0 {
		return []map[string]interface{}{}, nil
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].order != rows[j].order {
			return rows[i].order < rows[j].order
		}
		return i < j
	})

	out := make([]map[string]interface{}, 0, len(rows))
	for _, r := range rows {
		if r.phase == "" {
			return nil, fmt.Errorf("task %q has empty taskPhase", r.name)
		}
		id, ok := destNameToID[r.name]
		if !ok || id == 0 {
			return nil, fmt.Errorf("destination has no task named %q — migrate that task first, then re-run this workflow", r.name)
		}
		out = append(out, map[string]interface{}{
			"taskPhase": r.phase,
			"taskId":    id,
		})
	}
	return out, nil
}

func mapWorkflowOptionTypes(opt interface{}, codeToID map[string]int64) ([]interface{}, string) {
	arr, ok := opt.([]interface{})
	if !ok || len(arr) == 0 {
		return []interface{}{}, ""
	}
	var ids []interface{}
	var missing []string
	for _, e := range arr {
		switch v := e.(type) {
		case float64:
			ids = append(ids, int64(v))
		case int64:
			ids = append(ids, v)
		case json.Number:
			n, _ := v.Int64()
			ids = append(ids, n)
		case map[string]interface{}:
			code := strings.TrimSpace(stringFromAny(v["code"]))
			if code == "" {
				code = strings.TrimSpace(stringFromAny(v["name"]))
			}
			if id, ok := codeToID[code]; ok && id > 0 {
				ids = append(ids, id)
			} else if code != "" {
				missing = append(missing, code)
			}
		default:
			// unknown shape — skip
		}
	}
	warn := ""
	if len(missing) > 0 {
		warn = fmt.Sprintf("option types not found on destination by code: %s (create them or map manually)", strings.Join(missing, ", "))
	}
	return ids, warn
}

func isDuplicateErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "already exists") ||
		strings.Contains(s, "already in use") ||
		strings.Contains(s, "must be unique") ||
		strings.Contains(s, "422") ||
		strings.Contains(s, "duplicate")
}

// ---- integration resolution ----

func resolveTaskIntegrations(src, dst *morpheus.Client, task map[string]interface{}) error {
	code := taskTypeCode(task)
	opts, _ := task["taskOptions"].(map[string]interface{})

	// file.repository (Git)
	if file, ok := task["file"].(map[string]interface{}); ok {
		if strings.EqualFold(stringFromAny(file["sourceType"]), "repository") {
			repo, _ := file["repository"].(map[string]interface{})
			intName, err := resolveSourceGitIntegrationName(src, repo)
			if err != nil {
				return err
			}
			integ, err := findGitIntegrationByName(dst, intName)
			if err != nil {
				return fmt.Errorf("%w%s", err, sourceRepoIntegrationHint(src, repo))
			}
			if err := verifyIntegrationSSHKey(dst, integ); err != nil {
				return fmt.Errorf("%w%s", err, sourceRepoIntegrationHint(src, repo))
			}
			file["repository"] = map[string]interface{}{
				"id":   intFromAny(integ["id"]),
				"name": stringFromAny(integ["name"]),
			}
		}
	}

	// Ansible playbook: ansibleGitId references source integration id
	if code == "ansibleTask" && opts != nil {
		rawID := stringFromAny(opts["ansibleGitId"])
		if rawID == "" {
			return nil
		}
		srcID, err := strconv.ParseInt(strings.TrimSpace(rawID), 10, 64)
		if err != nil || srcID <= 0 {
			return fmt.Errorf("invalid ansibleGitId %q", rawID)
		}
		if src == nil || strings.TrimSpace(src.BaseURL) == "" {
			return fmt.Errorf("source Morpheus credentials required to resolve ansible integration id %d", srcID)
		}
		intName, err := integrationNameByID(src, srcID)
		if err != nil {
			return fmt.Errorf("ansible integration id %d on source: %w — fix source or provide matching ansible integration on destination", srcID, err)
		}
		integ, err := findAnsibleIntegrationByName(dst, intName)
		if err != nil {
			return fmt.Errorf("%w%s", err, sourceRepoIntegrationHint(src, map[string]interface{}{"id": srcID}))
		}
		if err := verifyIntegrationSSHKey(dst, integ); err != nil {
			return fmt.Errorf("%w%s", err, sourceRepoIntegrationHint(src, map[string]interface{}{"id": srcID}))
		}
		destID := intFromAny(integ["id"])
		opts["ansibleGitId"] = strconv.FormatInt(destID, 10)
	}

	// Shell/script repo via taskOptions.localScriptGitId
	if opts != nil && code != "ansibleTask" {
		rawID := strings.TrimSpace(stringFromAny(opts["localScriptGitId"]))
		if rawID == "" || rawID == "null" {
			return nil
		}
		srcID, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil || srcID <= 0 {
			return nil
		}
		if src == nil || strings.TrimSpace(src.BaseURL) == "" {
			return fmt.Errorf("source Morpheus credentials required to resolve localScriptGitId %d", srcID)
		}
		intName, err := integrationNameByID(src, srcID)
		if err != nil {
			return fmt.Errorf("git integration id %d (localScriptGitId): %w", srcID, err)
		}
		integ, err := findGitIntegrationByName(dst, intName)
		if err != nil {
			return fmt.Errorf("%w%s", err, sourceRepoIntegrationHint(src, map[string]interface{}{"id": srcID}))
		}
		if err := verifyIntegrationSSHKey(dst, integ); err != nil {
			return fmt.Errorf("%w%s", err, sourceRepoIntegrationHint(src, map[string]interface{}{"id": srcID}))
		}
		opts["localScriptGitId"] = strconv.FormatInt(intFromAny(integ["id"]), 10)
	}

	return nil
}

func integrationObjectByID(c *morpheus.Client, id int64) (map[string]interface{}, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid integration id %d", id)
	}
	body, err := c.GetRaw(fmt.Sprintf("/api/integrations/%d", id))
	if err != nil {
		return nil, err
	}
	var wrap map[string]json.RawMessage
	if json.Unmarshal(body, &wrap) != nil {
		return nil, fmt.Errorf("bad integrations response")
	}
	raw, ok := wrap["integration"]
	if !ok {
		return nil, fmt.Errorf("missing integration object")
	}
	var obj map[string]interface{}
	if json.Unmarshal(raw, &obj) != nil {
		return nil, fmt.Errorf("bad integration json")
	}
	return obj, nil
}

func integrationNameByID(c *morpheus.Client, id int64) (string, error) {
	obj, err := integrationObjectByID(c, id)
	if err != nil {
		return "", err
	}
	return stringFromAny(obj["name"]), nil
}

func sourceRepoIntegrationHint(src *morpheus.Client, repo map[string]interface{}) string {
	if src == nil || repo == nil {
		return ""
	}
	id := intFromAny(repo["id"])
	if id <= 0 {
		return ""
	}
	obj, err := integrationObjectByID(src, id)
	if err != nil {
		return fmt.Sprintf(" On source, repository referenced integration id %d (could not load: %v).", id, err)
	}
	iname := stringFromAny(obj["name"])
	sk, _ := obj["serviceKey"].(map[string]interface{})
	skName := strings.TrimSpace(stringFromAny(sk["name"]))
	skID := intFromAny(sk["id"])
	h := fmt.Sprintf(" On the source appliance, this repository uses Git integration %q (integration id %d).", iname, id)
	if skName != "" {
		h += fmt.Sprintf(" The SSH key pair on source is named %q (key id %d on source). Create or import that key on the destination, assign it to a Git integration that reaches this repo, then re-run.", skName, skID)
	} else {
		h += " No SSH key pair is listed on that source integration."
	}
	return h
}

func integrationTypeCode(m map[string]interface{}) string {
	tm, ok := m["integrationType"].(map[string]interface{})
	if ok {
		return strings.ToLower(stringFromAny(tm["code"]))
	}
	return strings.ToLower(stringFromAny(m["type"]))
}

func isGitIntegration(m map[string]interface{}) bool {
	code := integrationTypeCode(m)
	if code == "git" {
		return true
	}
	if strings.Contains(code, "git") {
		return true
	}
	return strings.EqualFold(stringFromAny(m["type"]), "git")
}

func isAnsibleIntegration(m map[string]interface{}) bool {
	code := integrationTypeCode(m)
	if strings.Contains(code, "ansible") {
		return true
	}
	t := strings.ToLower(stringFromAny(m["type"]))
	return strings.Contains(t, "ansible")
}

func resolveSourceGitIntegrationName(src *morpheus.Client, repo map[string]interface{}) (string, error) {
	if repo == nil {
		return "", fmt.Errorf("repository-backed task has no file.repository reference")
	}
	repoID := intFromAny(repo["id"])
	if src != nil && repoID > 0 {
		name, err := integrationNameByID(src, repoID)
		if err == nil && strings.TrimSpace(name) != "" {
			return name, nil
		}
		if err != nil {
			return "", fmt.Errorf("git integration id %d on source: %w", repoID, err)
		}
	}
	name := strings.TrimSpace(stringFromAny(repo["name"]))
	if name == "" {
		return "", fmt.Errorf("repository-backed task has no Git integration name on source")
	}
	lower := strings.ToLower(name)
	if strings.Contains(name, "://") || strings.HasPrefix(lower, "git@") || strings.Contains(lower, ".git") {
		if src == nil || strings.TrimSpace(src.BaseURL) == "" {
			return "", fmt.Errorf("source Morpheus credentials required to resolve Git integration id %d (repository host is %q, not an integration name)", repoID, name)
		}
		return "", fmt.Errorf("could not resolve Git integration name for repository id %d on source (host %q)", repoID, name)
	}
	return name, nil
}

func findGitIntegrationByName(dst *morpheus.Client, name string) (map[string]interface{}, error) {
	want := strings.ToLower(strings.TrimSpace(name))
	if want == "" {
		return nil, fmt.Errorf("empty Git integration name from source")
	}
	all, err := listIntegrationsAll(dst)
	if err != nil {
		return nil, fmt.Errorf("list integrations: %w", err)
	}
	var candidates []map[string]interface{}
	for _, m := range all {
		if !isGitIntegration(m) {
			continue
		}
		if strings.ToLower(stringFromAny(m["name"])) == want {
			return m, nil
		}
		candidates = append(candidates, m)
	}
	for _, m := range candidates {
		n := strings.ToLower(stringFromAny(m["name"]))
		if strings.Contains(n, want) || strings.Contains(want, n) {
			return m, nil
		}
	}
	return nil, fmt.Errorf("no Git integration on destination named like %q — create one matching the source integration name and SSH key, then re-run migration", name)
}

func listIntegrationsAll(dst *morpheus.Client) ([]map[string]interface{}, error) {
	raws, err := paginateList(dst, "/api/integrations", "integrations")
	if err != nil {
		return nil, err
	}
	var out []map[string]interface{}
	for _, raw := range raws {
		var m map[string]interface{}
		if json.Unmarshal(raw, &m) == nil {
			out = append(out, m)
		}
	}
	return out, nil
}

func findAnsibleIntegrationByName(dst *morpheus.Client, name string) (map[string]interface{}, error) {
	want := strings.ToLower(strings.TrimSpace(name))
	if want == "" {
		return nil, fmt.Errorf("empty ansible integration name from source")
	}
	all, err := listIntegrationsAll(dst)
	if err != nil {
		return nil, fmt.Errorf("list integrations: %w", err)
	}
	var candidates []map[string]interface{}
	for _, m := range all {
		if !isAnsibleIntegration(m) {
			continue
		}
		if strings.ToLower(stringFromAny(m["name"])) == want {
			return m, nil
		}
		candidates = append(candidates, m)
	}
	// prefix / contains
	for _, m := range candidates {
		n := strings.ToLower(stringFromAny(m["name"]))
		if strings.Contains(n, want) || strings.Contains(want, n) {
			return m, nil
		}
	}
	return nil, fmt.Errorf("no Ansible integration on destination named like %q — create one matching the source integration name and SSH key, then re-run migration", name)
}

func verifyIntegrationSSHKey(dst *morpheus.Client, integ map[string]interface{}) error {
	sk, ok := integ["serviceKey"].(map[string]interface{})
	if !ok || sk == nil {
		return nil
	}
	kid := intFromAny(sk["id"])
	kname := stringFromAny(sk["name"])
	if kid <= 0 {
		return nil
	}
	_, err := dst.GetRaw(fmt.Sprintf("/api/key-pairs/%d", kid))
	if err != nil {
		return fmt.Errorf("integration %q references SSH key pair id %d (%q) which is missing or inaccessible on destination (%v). Create/import this key pair, attach it to the integration, then re-run migration",
			stringFromAny(integ["name"]), kid, kname, err)
	}
	return nil
}

func stringFromAny(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case int64:
		return strconv.FormatInt(t, 10)
	case int:
		return strconv.Itoa(t)
	case bool:
		return strconv.FormatBool(t)
	case nil:
		return ""
	default:
		return fmt.Sprint(t)
	}
}

func intFromAny(v interface{}) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	case json.Number:
		n, _ := t.Int64()
		return n
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}
