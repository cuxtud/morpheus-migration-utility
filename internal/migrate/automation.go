package migrate

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/anish/morpheus-snapshot/internal/morpheus"
)

// automationState tracks destination IDs discovered during a migration run.
type automationState struct {
	destTaskNameToID      map[string]int64
	destWorkflowKeyToID   map[string]int64 // name + "\x00" + type
	destOptionCodeToID map[string]int64
	optionTypesLoadErr string
}

func newAutomationState() *automationState {
	return &automationState{
		destTaskNameToID:    map[string]int64{},
		destWorkflowKeyToID: map[string]int64{},
		destOptionCodeToID:  map[string]int64{},
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
	switch t {
	case "task":
		return 0
	case "workflow":
		return 1
	case "optionList":
		return 2
	case "input":
		return 3
	case "form":
		return 4
	default:
		return 99
	}
}

func (s *automationState) refreshDestTasks(dst *morpheus.Client) error {
	raws, err := paginateList(dst, "/api/tasks", "tasks")
	if err != nil {
		return err
	}
	s.destTaskNameToID = map[string]int64{}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		id := intFromAny(row["id"])
		name := stringFromAny(row["name"])
		if name != "" && id > 0 {
			s.destTaskNameToID[name] = id
		}
	}
	return nil
}

func (s *automationState) refreshDestWorkflows(dst *morpheus.Client) error {
	raws, err := paginateList(dst, "/api/task-sets", "taskSets")
	if err != nil {
		return err
	}
	s.destWorkflowKeyToID = map[string]int64{}
	for _, raw := range raws {
		var row map[string]interface{}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		id := intFromAny(row["id"])
		name := stringFromAny(row["name"])
		wt := stringFromAny(row["type"])
		if name != "" && id > 0 {
			s.destWorkflowKeyToID[workflowKey(name, wt)] = id
		}
	}
	return nil
}

func workflowKey(name, wtype string) string {
	return name + "\x00" + wtype
}

func (s *automationState) loadDestOptionTypes(dst *morpheus.Client) {
	if len(s.destOptionCodeToID) > 0 {
		return
	}
	s.fetchOptionTypesFromDestination(dst)
}

func (s *automationState) reloadDestOptionTypes(dst *morpheus.Client) {
	s.destOptionCodeToID = map[string]int64{}
	s.optionTypesLoadErr = ""
	s.fetchOptionTypesFromDestination(dst)
}

func (s *automationState) fetchOptionTypesFromDestination(dst *morpheus.Client) {
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

		if state.destTaskNameToID[r.name] > 0 {
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
		if state.destOptionCodeToID[code] > 0 {
			continue
		}

		id, err := createOptionTypeOnDestination(src, dst, om, code)
		if err != nil {
			return &ItemResult{Name: wfName, Type: "workflow", Status: "blocked", Message: fmt.Sprintf("could not create inputs field %q on destination: %v", code, err)}
		}
		state.destOptionCodeToID[code] = id
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

	state.reloadDestOptionTypes(dst)
	if existingID := state.destOptionCodeToID[code]; existingID > 0 {
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

	_, err := createOptionTypeOnDestination(src, dst, obj, code)
	if err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "blocked", Message: fmt.Sprintf("create input: %v", err)}
	}
	state.reloadDestOptionTypes(dst)
	return ItemResult{Name: name, Type: item.Type, Status: "success", Outcome: "created", Message: "Created input on destination"}
}

func migrateFormWithAutomation(src, dst *morpheus.Client, item SelectedItem, state *automationState) ItemResult {
	_ = state
	name := strings.TrimSpace(item.Name)
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(item.RawJSON), &raw); err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "error", Message: fmt.Sprintf("invalid form json: %v", err)}
	}

	obj := raw
	if wrapped, ok := raw["optionTypeForm"].(map[string]interface{}); ok && wrapped != nil {
		obj = wrapped
	}

	if src != nil && item.ID > 0 {
		if body, err := src.GetRaw(fmt.Sprintf("/api/library/option-type-forms/%d", item.ID)); err == nil {
			var wrap map[string]json.RawMessage
			if json.Unmarshal(body, &wrap) == nil {
				if rawForm, ok := wrap["optionTypeForm"]; ok {
					var fresh map[string]interface{}
					if json.Unmarshal(rawForm, &fresh) == nil && fresh != nil {
						obj = fresh
					}
				}
			}
		}
	}

	if n := strings.TrimSpace(stringFromAny(obj["name"])); n != "" {
		name = n
	}
	formCode := strings.TrimSpace(stringFromAny(obj["code"]))
	if formCode == "" {
		return ItemResult{Name: name, Type: item.Type, Status: "blocked", Message: "form has empty code — cannot migrate"}
	}

	existingID := findOptionTypeFormIDByCode(dst, formCode)
	var destForm map[string]interface{}
	if existingID > 0 {
		body, err := dst.GetRaw(fmt.Sprintf("/api/library/option-type-forms/%d", existingID))
		if err != nil {
			return ItemResult{Name: name, Type: item.Type, Status: "error", Message: fmt.Sprintf("load destination form: %v", err)}
		}
		var wrap map[string]json.RawMessage
		if json.Unmarshal(body, &wrap) == nil {
			if rawForm, ok := wrap["optionTypeForm"]; ok {
				var dm map[string]interface{}
				if json.Unmarshal(rawForm, &dm) == nil && dm != nil {
					destForm = dm
				}
			}
		}
	}

	payload, err := buildOptionTypeFormPayload(src, dst, obj, destForm)
	if err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "blocked", Message: fmt.Sprintf("prepare form: %v", err)}
	}

	if existingID > 0 {
		_, err = dst.PutRaw(fmt.Sprintf("/api/library/option-type-forms/%d", existingID), payload)
		if err != nil {
			return ItemResult{Name: name, Type: item.Type, Status: "error", Message: fmt.Sprintf("update form: %v", err)}
		}
		return ItemResult{Name: name, Type: item.Type, Status: "success", Outcome: "updated", Message: "Updated form on destination"}
	}

	body, err := dst.PostRaw("/api/library/option-type-forms", payload)
	if err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "blocked", Message: fmt.Sprintf("create form: %v", err)}
	}
	if parseOptionTypeFormIDFromResponse(body) <= 0 {
		if findOptionTypeFormIDByCode(dst, formCode) > 0 {
			return ItemResult{Name: name, Type: item.Type, Status: "skipped", Message: "Form may already exist on destination"}
		}
	}
	return ItemResult{Name: name, Type: item.Type, Status: "success", Outcome: "created", Message: "Created form on destination"}
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

	validCodes := collectFormOptionCodesLower(clone)
	for i := range metas {
		if cfg, ok := metas[i].opt["config"].(map[string]interface{}); ok {
			sanitizeDanglingFormConfigRefs(cfg, validCodes)
		}
	}

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

func findOptionTypeFormIDByCode(dst *morpheus.Client, code string) int64 {
	code = strings.TrimSpace(code)
	if code == "" {
		return 0
	}
	want := strings.ToLower(code)
	path := fmt.Sprintf("/api/library/option-type-forms?phrase=%s&max=100&offset=0", url.QueryEscape(code))
	body, err := dst.GetRaw(path)
	if err != nil {
		return 0
	}
	var wrap map[string]json.RawMessage
	if json.Unmarshal(body, &wrap) != nil {
		return 0
	}
	raw, ok := wrap["optionTypeForms"]
	if !ok {
		return 0
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return 0
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
	return 0
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

func ensureOptionTypeListDependency(src, dst *morpheus.Client, optionType map[string]interface{}) error {
	ol, ok := optionType["optionList"].(map[string]interface{})
	if !ok || ol == nil {
		return nil
	}
	srcListID := intFromAny(ol["id"])
	srcListName := strings.TrimSpace(stringFromAny(ol["name"]))
	if srcListName == "" && srcListID > 0 && src != nil {
		name, err := fetchSourceOptionTypeListName(src, srcListID)
		if err == nil {
			srcListName = name
		}
	}
	if srcListName == "" {
		// If we cannot identify the list by name, keep the original object.
		return nil
	}

	if dstID := findOptionTypeListIDByName(dst, srcListName); dstID > 0 {
		optionType["optionList"] = map[string]interface{}{"id": dstID}
		return nil
	}

	if src == nil || srcListID <= 0 {
		return fmt.Errorf("option list %q is not present on destination and source details are unavailable", srcListName)
	}

	srcObj, err := fetchSourceOptionTypeList(src, srcListID)
	if err != nil {
		return fmt.Errorf("option list %q is not present on destination and could not be loaded from source id %d: %v", srcListName, srcListID, err)
	}
	dstID, err := createOptionTypeListOnDestination(dst, srcObj)
	if err != nil {
		return fmt.Errorf("failed to create option list %q on destination: %v", srcListName, err)
	}
	optionType["optionList"] = map[string]interface{}{"id": dstID}
	return nil
}

func fetchSourceOptionTypeListName(src *morpheus.Client, id int64) (string, error) {
	obj, err := fetchSourceOptionTypeList(src, id)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(stringFromAny(obj["name"]))
	if name == "" {
		return "", fmt.Errorf("option list id %d has empty name", id)
	}
	return name, nil
}

func fetchSourceOptionTypeList(src *morpheus.Client, id int64) (map[string]interface{}, error) {
	paths := []string{
		fmt.Sprintf("/api/library/option-type-lists/%d", id),
		fmt.Sprintf("/api/option-type-lists/%d", id),
	}
	var lastErr error
	for _, p := range paths {
		body, err := src.GetRaw(p)
		if err != nil {
			lastErr = err
			continue
		}
		var wrap map[string]json.RawMessage
		if err := json.Unmarshal(body, &wrap); err != nil {
			lastErr = err
			continue
		}
		raw, ok := wrap["optionTypeList"]
		if !ok {
			lastErr = fmt.Errorf("missing optionTypeList key in response")
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal(raw, &obj); err != nil {
			lastErr = err
			continue
		}
		return obj, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("could not fetch option list id %d", id)
}

func findOptionTypeListIDByName(dst *morpheus.Client, name string) int64 {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0
	}
	paths := []string{
		fmt.Sprintf("/api/library/option-type-lists?phrase=%s&max=100&offset=0", url.QueryEscape(name)),
		fmt.Sprintf("/api/option-type-lists?phrase=%s&max=100&offset=0", url.QueryEscape(name)),
	}
	want := strings.ToLower(name)
	for _, p := range paths {
		body, err := dst.GetRaw(p)
		if err != nil {
			continue
		}
		var wrap map[string]json.RawMessage
		if json.Unmarshal(body, &wrap) != nil {
			continue
		}
		raw, ok := wrap["optionTypeLists"]
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
			n := strings.ToLower(strings.TrimSpace(stringFromAny(row["name"])))
			if n != want {
				continue
			}
			if id := intFromAny(row["id"]); id > 0 {
				return id
			}
		}
	}
	return 0
}

func createOptionTypeListOnDestination(dst *morpheus.Client, srcObj map[string]interface{}) (int64, error) {
	var clone map[string]interface{}
	raw, err := json.Marshal(srcObj)
	if err != nil {
		return 0, err
	}
	if err := json.Unmarshal(raw, &clone); err != nil {
		return 0, err
	}
	for _, k := range []string{
		"id", "dateCreated", "lastUpdated", "createdBy", "account",
		"owner", "stats", "uuid",
	} {
		delete(clone, k)
	}

	endpoints := []string{"/api/library/option-type-lists", "/api/option-type-lists"}
	var lastErr error
	for _, ep := range endpoints {
		payload, err := json.Marshal(map[string]interface{}{"optionTypeList": clone})
		if err != nil {
			lastErr = err
			continue
		}
		body, err := dst.PostRaw(ep, payload)
		if err != nil {
			lastErr = err
			continue
		}
		if id := parseOptionTypeListIDFromResponse(body); id > 0 {
			return id, nil
		}
		if id := findOptionTypeListIDByName(dst, stringFromAny(clone["name"])); id > 0 {
			return id, nil
		}
		lastErr = fmt.Errorf("response missing optionTypeList id")
	}
	if lastErr != nil {
		return 0, lastErr
	}
	return 0, fmt.Errorf("could not create option type list")
}

func parseOptionTypeListIDFromResponse(body []byte) int64 {
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
	raw, ok := wrap["optionTypeList"]
	if !ok {
		return 0
	}
	var row map[string]interface{}
	if json.Unmarshal(raw, &row) != nil {
		return 0
	}
	return intFromAny(row["id"])
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

	existingID := state.destTaskNameToID[name]
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
			if id := state.destTaskNameToID[name]; id > 0 {
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
	tid := state.destTaskNameToID[taskName]
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

	tasksPayload, err := buildWorkflowTasksPayload(obj, state.destTaskNameToID)
	if err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "blocked", Message: err.Error()}
	}

	state.reloadDestOptionTypes(dst)
	optIDs, optWarn := mapWorkflowOptionTypes(obj["optionTypes"], state.destOptionCodeToID)

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
	if state.optionTypesLoadErr != "" && len(optIDs) == 0 && obj["optionTypes"] != nil {
		if arr, ok := obj["optionTypes"].([]interface{}); ok && len(arr) > 0 {
			msgExtra = msgExtra + "; " + state.optionTypesLoadErr
		}
	}

	if err := state.refreshDestWorkflows(dst); err != nil {
		return ItemResult{Name: name, Type: item.Type, Status: "error", Message: fmt.Sprintf("list workflows: %v", err)}
	}
	wfKey := workflowKey(name, wfType)
	existingID := state.destWorkflowKeyToID[wfKey]

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
				if id := state.destWorkflowKeyToID[wfKey]; id > 0 {
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
	s := err.Error()
	return strings.Contains(strings.ToLower(s), "already exists") ||
		strings.Contains(s, "422") ||
		strings.Contains(strings.ToLower(s), "duplicate")
}

// ---- integration resolution ----

func resolveTaskIntegrations(src, dst *morpheus.Client, task map[string]interface{}) error {
	code := taskTypeCode(task)
	opts, _ := task["taskOptions"].(map[string]interface{})

	// file.repository (Git)
	if file, ok := task["file"].(map[string]interface{}); ok {
		if strings.EqualFold(stringFromAny(file["sourceType"]), "repository") {
			repo, _ := file["repository"].(map[string]interface{})
			repoName := stringFromAny(repo["name"])
			if repoName == "" {
				return fmt.Errorf("repository-backed task has no file.repository.name — fix source task")
			}
			integ, err := findGitIntegrationForRepository(dst, src, repoName, repo)
			if err != nil {
				return err
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
		integ, err := findGitIntegrationForRepository(dst, src, intName, map[string]interface{}{"id": srcID})
		if err != nil {
			return err
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

func findGitIntegrationForRepository(dst *morpheus.Client, src *morpheus.Client, repoName string, sourceRepo map[string]interface{}) (map[string]interface{}, error) {
	if strings.TrimSpace(repoName) == "" {
		return nil, fmt.Errorf("empty repository / integration name")
	}
	phrases := integrationSearchPhrases(repoName)
	var lastErr error
	for _, ph := range phrases {
		path := "/api/integrations?phrase=" + url.QueryEscape(ph) + "&max=100&offset=0"
		body, err := dst.GetRaw(path)
		if err != nil {
			lastErr = err
			continue
		}
		list, err := parseIntegrationsList(body)
		if err != nil {
			lastErr = err
			continue
		}
		if picked := pickGitIntegration(list, repoName); picked != nil {
			return picked, nil
		}
	}
	all, err := listIntegrationsAll(dst)
	if err != nil && lastErr != nil {
		return nil, fmt.Errorf("find Git integration for %q: %v (phrase search failed: %v)%s", repoName, err, lastErr, sourceRepoIntegrationHint(src, sourceRepo))
	}
	if err == nil {
		if picked := pickGitIntegration(all, repoName); picked != nil {
			return picked, nil
		}
	}
	return nil, fmt.Errorf("no Git integration on destination matches repository %q (tried phrases %v). Add a Git integration for this repo and matching SSH key.%s", repoName, phrases, sourceRepoIntegrationHint(src, sourceRepo))
}

func integrationSearchPhrases(repoName string) []string {
	s := strings.TrimSpace(repoName)
	out := []string{s}
	if u := strings.SplitN(s, "_", 2); len(u) > 0 && u[0] != "" && u[0] != s {
		out = append(out, u[0])
	}
	if len(s) > 6 {
		out = append(out, s[:6])
	}
	// dedupe
	seen := map[string]struct{}{}
	var deduped []string
	for _, p := range out {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		deduped = append(deduped, p)
	}
	return deduped
}

func parseIntegrationsList(body []byte) ([]map[string]interface{}, error) {
	var wrap map[string]json.RawMessage
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, err
	}
	raw, ok := wrap["integrations"]
	if !ok {
		return nil, fmt.Errorf("no integrations key")
	}
	var list []map[string]interface{}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	return list, nil
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

func pickGitIntegration(list []map[string]interface{}, repoName string) map[string]interface{} {
	var gitOnly []map[string]interface{}
	for _, m := range list {
		if isGitIntegration(m) {
			gitOnly = append(gitOnly, m)
		}
	}
	if len(gitOnly) == 0 {
		return nil
	}
	repoLower := strings.ToLower(strings.TrimSpace(repoName))
	bestScore := -1
	var best map[string]interface{}
	for _, m := range gitOnly {
		n := strings.ToLower(stringFromAny(m["name"]))
		score := 0
		switch {
		case n == repoLower:
			score = 100
		case len(repoLower) >= 2 && len(n) >= 2 && (strings.Contains(n, repoLower) || strings.Contains(repoLower, n)):
			score = 60
		}
		if score > bestScore {
			bestScore = score
			best = m
		}
	}
	// Do not pick an unrelated Git integration (would create a broken repository link).
	if bestScore >= 60 {
		return best
	}
	return nil
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
