package migrate

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/cuxtud/morpheus-migration-utility/internal/morpheus"
)

const defaultCatalogParallelism = 4

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
		Phase:   "item_done",
		Index:   idx,
		Total:   total,
		Message: fmt.Sprintf("%s — %s", label, doneMsg),
		Status:  itemResult.Status,
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
	itemResult := migrateOneItem(migrateItemContext{src: src, dst: dst, state: state, item: item, label: label})
	r.record(idx, total, label, itemResult)
}

func (r *migrationRecorder) runCatalogItemsParallel(src, dst *morpheus.Client, catalogs []SelectedItem, workers, total int, baseIndex int) {
	r.emitSafe(ProgressEvent{
		Phase:   "step",
		Message: fmt.Sprintf("Migrating %d catalog items in parallel (%d workers)…", len(catalogs), workers),
		Total:   total,
	})

	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for _, item := range catalogs {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			label := itemQueueLabel(item)
			r.emitSafe(ProgressEvent{
				Phase:   "step",
				Total:   total,
				Message: fmt.Sprintf("Migrating %s (parallel)…", label),
			})

			workerState := newAutomationState()
			workerState.progress = func(ev ProgressEvent) {
				if ev.Message != "" {
					ev.Message = label + ": " + ev.Message
				}
				r.emitSafe(ev)
			}

			itemResult := migrateOneItem(migrateItemContext{
				src: src, dst: dst, state: workerState, item: item, label: label,
			})

			n := int(r.doneCount.Add(1)) + baseIndex
			r.record(n, total, label, itemResult)
		}()
	}
	wg.Wait()
}
