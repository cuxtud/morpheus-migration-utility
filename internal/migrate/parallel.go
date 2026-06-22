package migrate

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cuxtud/morpheus-migration-utility/internal/morpheus"
)

const defaultCatalogParallelism = 4
const defaultWaveParallelism = 4

func migrationParallelWorkers(req MigrateRequest) int {
	n := req.ParallelWorkers
	if n == 1 {
		return 1
	}
	if n <= 0 {
		n = defaultWaveParallelism
	}
	if n > defaultWaveParallelism {
		n = defaultWaveParallelism
	}
	return n
}

// groupMigrationWaves splits sorted items into waves that share the same dependency tier.
func groupMigrationWaves(items []SelectedItem) [][]SelectedItem {
	sorted := sortItemsForMigration(items)
	if len(sorted) == 0 {
		return nil
	}
	var waves [][]SelectedItem
	current := []SelectedItem{sorted[0]}
	lastOrder := itemTypeOrder(sorted[0].Type)
	for _, it := range sorted[1:] {
		order := itemTypeOrder(it.Type)
		if order != lastOrder {
			waves = append(waves, current)
			current = nil
			lastOrder = order
		}
		current = append(current, it)
	}
	if len(current) > 0 {
		waves = append(waves, current)
	}
	return waves
}

// catalogParallelWorkers returns worker count for catalog item migration (0 = auto).
func catalogParallelWorkers(req MigrateRequest, catalogCount int) int {
	if catalogCount <= 1 {
		return 1
	}
	n := req.ParallelCatalog
	if n == 1 {
		return 1
	}
	if n <= 0 {
		n = defaultCatalogParallelism
	}
	if n > catalogCount {
		n = catalogCount
	}
	return n
}

func partitionCatalogItems(items []SelectedItem) (serial, catalogs []SelectedItem) {
	for _, it := range items {
		if normalizeType(it.Type) == "catalogItem" {
			catalogs = append(catalogs, it)
		} else {
			serial = append(serial, it)
		}
	}
	return serial, catalogs
}

type migrateItemContext struct {
	src   *morpheus.Client
	dst   *morpheus.Client
	state *automationState
	item  SelectedItem
	label string
}

func migrateOneItem(ctx migrateItemContext) ItemResult {
	item := ctx.item
	src, dst, state := ctx.src, ctx.dst, ctx.state

	switch normalizeType(item.Type) {
	case "task":
		return migrateTaskWithAutomation(src, dst, item, state)
	case "workflow":
		return migrateWorkflowWithAutomation(src, dst, item, state)
	case "input":
		return migrateInputWithAutomation(src, dst, item, state)
	case "form":
		return migrateFormWithAutomation(src, dst, item, state)
	case "optionList":
		return migrateOptionListWithAutomation(src, dst, item, state)
	case "cypher":
		return migrateCypher(src, dst, item)
	case "instanceType":
		return migrateInstanceTypeWithAutomation(src, dst, item, state)
	case "credential":
		return migrateCredentialItem(src, dst, item)
	case "group":
		return migrateGroupWithClouds(src, dst, item)
	case "cloud":
		return migrateCloudWithCredential(src, dst, item)
	case "catalogItem":
		return migrateCatalogItem(src, dst, item, state)
	case "integration":
		return skipNonMigratableItem(item)
	default:
		return migrateGenericEndpoint(dst, item, state, ctx.label)
	}
}

func migrateGenericEndpoint(dst *morpheus.Client, item SelectedItem, state *automationState, label string) ItemResult {
	spec, ok := endpointMap[normalizeType(item.Type)]
	if !ok {
		return ItemResult{
			Name:    item.Name,
			Type:    item.Type,
			Status:  "skipped",
			Message: fmt.Sprintf("Migration of type '%s' not yet supported", item.Type),
		}
	}

	var payload []byte
	var err error
	if item.Type == "nodeType" {
		payload, err = buildNodeTypePayloadWithVirtualImage(dst, item.RawJSON, spec)
	} else {
		payload, err = buildPayload(item.RawJSON, spec)
	}
	if err != nil {
		status := "error"
		if item.Type == "nodeType" && strings.Contains(strings.ToLower(err.Error()), "virtual image") {
			status = "blocked"
		}
		return ItemResult{
			Name:    item.Name,
			Type:    item.Type,
			Status:  status,
			Message: fmt.Sprintf("Failed to build payload: %v", err),
		}
	}

	if state != nil {
		state.reportStep(fmt.Sprintf("Creating %s on destination", label))
	}
	_, err = dst.PostRaw(spec.endpoint, payload)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "422") {
			return ItemResult{
				Name:    item.Name,
				Type:    item.Type,
				Status:  "skipped",
				Message: "Already exists on destination",
			}
		}
		return ItemResult{
			Name:    item.Name,
			Type:    item.Type,
			Status:  "error",
			Message: err.Error(),
		}
	}
	return ItemResult{
		Name:    item.Name,
		Type:    item.Type,
		Status:  "success",
		Outcome: "created",
	}
}

type migrationRecorder struct {
	result    *MigrateResult
	emit      func(ProgressEvent)
	progressMu sync.Mutex
	resultMu   sync.Mutex
	doneCount  atomic.Int32
}

func newMigrationRecorder(result *MigrateResult, emit func(ProgressEvent)) *migrationRecorder {
	return &migrationRecorder{result: result, emit: emit}
}

func (r *migrationRecorder) emitSafe(ev ProgressEvent) {
	if r.emit == nil {
		return
	}
	r.progressMu.Lock()
	r.emit(ev)
	r.progressMu.Unlock()
}

func (r *migrationRecorder) record(idx, total int, label string, itemResult ItemResult) {
	r.resultMu.Lock()
	appendItemResult(r.result, itemResult)
	r.resultMu.Unlock()

	doneMsg := strings.TrimSpace(itemResult.Message)
	if doneMsg == "" {
		doneMsg = itemResult.Status
	}
	r.emitSafe(ProgressEvent{
		Phase:      "item_done",
		Index:      idx,
		Total:      total,
		Message:    fmt.Sprintf("%s — %s", label, doneMsg),
		Status:     itemResult.Status,
		DurationMs: itemResult.DurationMs,
	})
}

func (r *migrationRecorder) runItem(src, dst *morpheus.Client, state *automationState, item SelectedItem, idx, total int) {
	label := itemQueueLabel(item)
	r.emitSafe(ProgressEvent{
		Phase:   "step",
		Index:   idx,
		Total:   total,
		Message: fmt.Sprintf("Migrating %s…", label),
	})
	itemStart := time.Now()
	state.beginItemDebug()
	itemResult := migrateOneItem(migrateItemContext{src: src, dst: dst, state: state, item: item, label: label})
	itemResult = state.finishItemResult(itemResult)
	itemResult.DurationMs = time.Since(itemStart).Milliseconds()
	r.record(idx, total, label, itemResult)
}

func (r *migrationRecorder) runWave(src, dst *morpheus.Client, state *automationState, wave []SelectedItem, workers, total, baseIndex int) int {
	if len(wave) == 0 {
		return baseIndex
	}
	if len(wave) == 1 || workers <= 1 {
		idx := baseIndex
		for _, item := range wave {
			idx++
			r.runItem(src, dst, state, item, idx, total)
		}
		return idx
	}
	r.doneCount.Store(0)
	r.runItemsParallel(src, dst, state, wave, workers, total, baseIndex, "wave")
	return baseIndex + len(wave)
}

func (r *migrationRecorder) runItemsParallel(src, dst *morpheus.Client, state *automationState, items []SelectedItem, workers, total, baseIndex int, kind string) {
	if len(items) == 0 {
		return
	}
	msg := fmt.Sprintf("Migrating %d items in parallel (%d workers)…", len(items), workers)
	if kind == "parallel" {
		msg = fmt.Sprintf("Migrating %d catalog items in parallel (%d workers)…", len(items), workers)
	}
	r.emitSafe(ProgressEvent{
		Phase:   "step",
		Message: msg,
		Total:   total,
	})

	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for _, item := range items {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			label := itemQueueLabel(item)
			parallelLabel := "parallel"
			if kind == "wave" {
				parallelLabel = "wave"
			}
			r.emitSafe(ProgressEvent{
				Phase:   "step",
				Total:   total,
				Message: fmt.Sprintf("Migrating %s (%s)…", label, parallelLabel),
			})

			itemStart := time.Now()
			state.beginItemDebug()
			itemResult := migrateOneItem(migrateItemContext{
				src: src, dst: dst, state: state, item: item, label: label,
			})
			itemResult = state.finishItemResult(itemResult)
			itemResult.DurationMs = time.Since(itemStart).Milliseconds()

			n := int(r.doneCount.Add(1)) + baseIndex
			r.record(n, total, label, itemResult)
		}()
	}
	wg.Wait()
}
