package execution

import (
	"net/http"

	"github.com/edcrove/provenly/backend/internal/platform/httpx"
)

// LiveCaseDTO is the wire form of LiveCase.
type LiveCaseDTO struct {
	TestCaseID  int64  `json:"testCaseId"`
	TestCaseKey string `json:"testCaseKey"`
	State       string `json:"state"`
}

// MismatchDTO is the wire form of Mismatch.
type MismatchDTO struct {
	Kind                string  `json:"kind"`
	TestCaseID          *int64  `json:"testCaseId"`
	TestCaseKey         *string `json:"testCaseKey"`
	RequestedTestCaseID *string `json:"requestedTestCaseId"`
	LiveStatus          *string `json:"liveStatus"`
	FinalStatus         *string `json:"finalStatus"`
}

// LiveDTO is the wire form of Live.
type LiveDTO struct {
	Reconciliation string        `json:"reconciliation"`
	Events         int32         `json:"events"`
	LastSequence   *int64        `json:"lastSequence"`
	RunFinished    bool          `json:"runFinished"`
	Waiting        int32         `json:"waiting"`
	Running        int32         `json:"running"`
	Finished       int32         `json:"finished"`
	TestCases      []LiveCaseDTO `json:"testCases"`
	Mismatches     []MismatchDTO `json:"mismatches"`
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func liveDTO(l Live, keys map[int64]string) LiveDTO {
	dto := LiveDTO{Reconciliation: l.Reconciliation, Events: l.Events, LastSequence: l.LastSequence, RunFinished: l.RunFinished,
		Waiting: l.Waiting, Running: l.Running, Finished: l.Finished,
		TestCases: make([]LiveCaseDTO, len(l.Cases)), Mismatches: make([]MismatchDTO, len(l.Mismatches))}
	for i, c := range l.Cases {
		dto.TestCases[i] = LiveCaseDTO{TestCaseID: c.TestCaseID, TestCaseKey: keys[c.TestCaseID], State: c.State}
	}
	for i, m := range l.Mismatches {
		d := MismatchDTO{Kind: m.Kind, TestCaseID: m.TestCaseID, RequestedTestCaseID: optional(m.Requested),
			LiveStatus: optional(m.LiveStatus), FinalStatus: optional(m.FinalStatus)}
		if m.TestCaseID != nil {
			d.TestCaseKey = optional(keys[*m.TestCaseID])
		}
		dto.Mismatches[i] = d
	}
	return dto
}

func (h *Handler) live(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.PathID(r, "testRunId")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	l, err := h.api.Live(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var ids []int64
	for _, c := range l.Cases {
		ids = append(ids, c.TestCaseID)
	}
	for _, m := range l.Mismatches {
		if m.TestCaseID != nil {
			ids = append(ids, *m.TestCaseID)
		}
	}
	keys, err := h.catalog.Keys(r.Context(), ids)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, liveDTO(l, keys))
}
