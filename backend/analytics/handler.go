package analytics

import (
	"encoding/json"
	"net/http"

	"github.com/garinesaiajay/commerceos/audit"
)

// Handler serves the dashboard API.
type Handler struct {
	metrics    *Service
	experiment *ExperimentService
	verifier   *audit.Verifier
}

func NewHandler(metrics *Service, experiment *ExperimentService, verifier *audit.Verifier) *Handler {
	return &Handler{metrics: metrics, experiment: experiment, verifier: verifier}
}

// Overview handles GET /dashboard/overview — a source-labelled merchant
// dashboard read model. It is intentionally separate from simulated reports.
func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	integrity := AuditIntegrity{Verified: false, ChainBroken: false}
	if h.verifier != nil {
		result, err := h.verifier.Verify(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		integrity = AuditIntegrity{
			Verified: result.Verified, ChainBroken: result.ChainBroken,
			RowsChecked: result.RowsChecked, BrokenAtID: result.BrokenAtID,
		}
	}

	overview, err := h.metrics.Overview(r.Context(), integrity)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

// Metrics handles GET /dashboard/metrics — real numbers from DB rows.
func (h *Handler) Metrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	m, err := h.metrics.Compute(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, m)
}

// Experiment handles POST /dashboard/experiment — runs a simulated A/B.
func (h *Handler) Experiment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name      string  `json:"name"`
		Seed      int64   `json:"seed"`
		Treatment float64 `json:"treatment_multiplier"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.Seed == 0 {
		req.Seed = 42
	}
	if req.Treatment == 0 {
		req.Treatment = 1.0
	}

	report, err := h.experiment.Run(r.Context(), req.Name, req.Seed, req.Treatment)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, report)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
