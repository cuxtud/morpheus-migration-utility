package migrate

import "fmt"

// ProgressEvent is emitted during migration for live UI updates.
type ProgressEvent struct {
	Phase   string   `json:"phase"` // preparing, queue, step, item_done
	Message string   `json:"message,omitempty"`
	Index   int      `json:"index,omitempty"`
	Total   int      `json:"total,omitempty"`
	Queue   []string `json:"queue,omitempty"`
	Status  string   `json:"status,omitempty"`
}

// ProgressFunc receives migration progress updates.
type ProgressFunc func(ProgressEvent)

func (s *automationState) reportStep(msg string) {
	if s == nil || s.progress == nil || msg == "" {
		return
	}
	s.progress(ProgressEvent{Phase: "step", Message: msg})
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
