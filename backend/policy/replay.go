package policy

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Run is one agent run reconstructed from persisted records (replay).
type Run struct {
	ID            string    `json:"run_id"`
	Action        string    `json:"action"`
	Amount        int64     `json:"amount"`
	Currency      string    `json:"currency"`
	Merchant      string    `json:"merchant"`
	Items         []string  `json:"items"`
	Decision      string    `json:"decision"`
	Reason        string    `json:"reason,omitempty"`
	FailedCheck   string    `json:"failed_check,omitempty"`
	Authorization string    `json:"authorization_id,omitempty"`
	AuthStatus    string    `json:"authorization_status,omitempty"`
	CreatedAt     time.Time `json:"created_at"`

	// Steps is the run's forensic timeline, one entry per persisted
	// stage (proposed -> risk-assessed -> policy-evaluated -> authorized
	// -> consumed). Only GetRun populates it -- ListRuns stays a flat
	// summary row, since building every run's timeline on every list
	// call would be a real N+1 query cost for no benefit until someone
	// opens that specific run. There is no persisted search/filter/rank
	// trail from the buyer/growth agents to add here yet; this is the
	// honest granularity the system actually captures today.
	Steps []RunStep `json:"steps,omitempty"`
}

// RunStep is one timestamped stage in a run's timeline.
type RunStep struct {
	Stage     string    `json:"stage"`
	Detail    string    `json:"detail"`
	Timestamp time.Time `json:"timestamp"`
}

// HandleListRuns serves GET /runs (replay list).
func (h *Handler) HandleListRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	runs, err := h.service.ListRuns(r.Context(), limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if runs == nil {
		runs = []Run{}
	}
	writeJSON(w, http.StatusOK, runs)
}

// HandleGetRun serves GET /runs/{run_id}.
func (h *Handler) HandleGetRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/runs/")
	id = strings.Trim(id, "/")
	if id == "" {
		http.Error(w, "run ID required", http.StatusBadRequest)
		return
	}
	run, err := h.service.GetRun(r.Context(), id)
	if err != nil {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, run)
}
