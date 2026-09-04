package analytics

import (
	"encoding/json"
	"net/http"

	"github.com/garinesaiajay/commerceos/audit"
	"github.com/garinesaiajay/commerceos/auth"
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
// dashboard read model. It is intentionally separate from simulated
// reports. Scoped to the calling operator's own merchant (P0 security
// fix, full-codebase re-audit 2026-09-04) -- this handler previously
// never checked auth.OperatorFromContext at all, so despite
// RequireOperator gating this route at the authentication layer
// (main.go), there was no authorization check here: any authenticated
// operator of any merchant saw the entire platform's revenue, orders,
// and raw audit log. See Service.Compute/Overview's doc comments.
func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	operator, ok := auth.OperatorFromContext(r.Context())
	if !ok {
		http.Error(w, "operator session required", http.StatusUnauthorized)
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

	overview, err := h.metrics.Overview(r.Context(), operator.MerchantID, integrity)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

// Metrics handles GET /dashboard/metrics — real numbers from DB rows,
// scoped to the calling operator's own merchant. Same P0 fix as
// Overview above -- this handler had the identical gap.
func (h *Handler) Metrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	operator, ok := auth.OperatorFromContext(r.Context())
	if !ok {
		http.Error(w, "operator session required", http.StatusUnauthorized)
		return
	}

	m, err := h.metrics.Compute(r.Context(), operator.MerchantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, m)
}

// ListExperiments handles GET /dashboard/experiments — the persisted
// history the Experiment handler already writes to on every run
// (analytics/experiment.go's Run upserts into the `experiments` table),
// previously never exposed: the dashboard could only ever see the
// single most-recently-run report, in local frontend state, lost on
// refresh -- every other list-shaped dashboard page (Campaigns,
// Approvals, Runs, Safety) persists and lists its history; this brings
// Analytics in line with that pattern.
func (h *Handler) ListExperiments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	reports, err := h.experiment.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, reports)
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

	// Two independent defaults, deliberately NOT combined into one `||`
	// condition (a prior version did exactly that -- `if req.Name == ""
	// || req.Seed == 0 { req.Seed = 42 }` -- which had two real bugs: an
	// empty name was never actually defaulted to anything, so it sailed
	// through to Run/ExperimentReport.ID as literal "exp_" (every
	// unnamed run silently overwrote the same row instead of getting a
	// usable identity), and a request that legitimately named an
	// experiment but omitted seed had ITS seed silently reset to 42 even
	// when seed 0 was never the actual condition that should have
	// triggered it for name-less requests). Matches the frontend's own
	// default experiment name (frontend/app/dashboard/analytics/
	// page.tsx's useState("ai_cross_sell")) so a bare POST with no body
	// at all behaves the same as loading the dashboard and clicking
	// "Run experiment" without changing anything.
	if req.Name == "" {
		req.Name = "ai_cross_sell"
	}
	if req.Seed == 0 {
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
