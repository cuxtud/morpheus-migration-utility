package migrate

import (
	"fmt"
	"strings"
)

// ProgressEvent is emitted during migration for live UI updates.
type ProgressEvent struct {
	Phase      string   `json:"phase"` // preparing, queue, step, item_done
	Message    string   `json:"message,omitempty"`
	Index      int      `json:"index,omitempty"`
	Total      int      `json:"total,omitempty"`
	Queue      []string `json:"queue,omitempty"`
	Status     string   `json:"status,omitempty"`
	DurationMs int64    `json:"durationMs,omitempty"`
}

// ProgressFunc receives migration progress updates.
type ProgressFunc func(ProgressEvent)

func (s *automationState) reportStep(msg string) {
	if s == nil || msg == "" {
		return
	}
	if s.progress != nil {
		s.progress(ProgressEvent{Phase: "step", Message: msg})
	}
}

func (s *automationState) beginItemDebug() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.itemDebugLines = nil
	s.mu.Unlock()
}

func (s *automationState) itemDebug(msg string) {
	if s == nil || strings.TrimSpace(msg) == "" {
		return
	}
	s.mu.Lock()
	s.itemDebugLines = append(s.itemDebugLines, msg)
	s.mu.Unlock()
	s.reportStep(msg)
}

func (s *automationState) finishItemResult(res ItemResult) ItemResult {
	if s == nil {
		return res
	}
	s.mu.Lock()
	lines := s.itemDebugLines
	s.itemDebugLines = nil
	s.mu.Unlock()
	if len(lines) > 0 {
		res.Details = append(res.Details, lines...)
	}
	return res
}

func itemQueueLabel(it SelectedItem) string {
	t := normalizeType(it.Type)
	if t == "" {
		t = it.Type
	}
	name := it.Name
	if name == "" {
		name = fmt.Sprintf("#%d", it.ID)
	}
	return fmt.Sprintf("%s: %s", t, name)
}
