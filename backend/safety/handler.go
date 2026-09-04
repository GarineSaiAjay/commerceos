package safety

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Handler exposes the safety/red-team endpoints.
type Handler struct {
	runner *Runner
	store  *Store
}

func NewHandler(runner *Runner, store *Store) *Handler {
	return &Handler{runner: runner, store: store}
}

// stripSuffix trims a path prefix and trailing action suffix, e.g.
// "/safety/attacks/att_01/run" -> "att_01".
func stripSuffix(path, prefix, suffix string) string {
	s := strings.TrimPrefix(path, prefix)
	s = strings.TrimSuffix(s, suffix)
	return strings.Trim(s, "/")
}

// ListAttacks serves GET /safety/attacks.
func (h *Handler) ListAttacks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, AttackLibrary)
}

// RunAttack serves POST /safety/attacks/{id}/run. It executes the attack
// through the real policy pipeline and returns the evidence.
func (h *Handler) RunAttack(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := stripSuffix(r.URL.Path, "/safety/attacks/", "/run")
	attack, ok := GetAttack(id)
	if !ok {
		http.Error(w, "attack not found", http.StatusNotFound)
		return
	}

	// No mandate_id from the request body anymore: the runner provisions
	// a fresh, real mandate itself (Runner.ensureRedTeamMandate) so every
	// attack is actually evaluated against its claimed guard instead of
	// failing generically on a caller-supplied ID that was never real
	// (the old default, the literal string "mnd_demo", was never seeded
	// anywhere and could never arise from POST /policy/mandates).
	res, err := h.runner.RunAttack(r.Context(), attack)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// RunSuite serves POST /safety/evaluations/run — runs the whole attack
// library and persists the evaluation.
func (h *Handler) RunSuite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Same reasoning as RunAttack above: the runner provisions its own
	// fresh, real mandate per attack now, so no mandate_id is read from
	// the request body here either.
	eval, err := h.runner.RunSuite(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if h.store != nil {
		_ = h.store.SaveEvaluation(r.Context(), eval)
	}
	writeJSON(w, http.StatusOK, eval)
}

// ListEvaluations serves GET /safety/evaluations.
func (h *Handler) ListEvaluations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	evals, err := h.store.ListEvaluations(r.Context(), 20)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if evals == nil {
		evals = []Evaluation{}
	}
	writeJSON(w, http.StatusOK, evals)
}

// GetEvaluation serves GET /safety/evaluations/{id}.
func (h *Handler) GetEvaluation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/safety/evaluations/")
	id = strings.Trim(id, "/")
	if id == "" {
		http.Error(w, "evaluation ID required", http.StatusBadRequest)
		return
	}
	eval, err := h.store.GetEvaluation(r.Context(), id)
	if err != nil {
		http.Error(w, "evaluation not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, eval)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
