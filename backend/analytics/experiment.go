package analytics

import (
	"context"
	"fmt"
	"math"

	"github.com/garinesaiajay/commerceos/growth"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ExperimentReport is a causal-style A/B result with a real CI.
type ExperimentReport struct {
	ID             string  `json:"experiment_id"`
	Name           string  `json:"name"`
	Metric         string  `json:"metric"`
	Population     int     `json:"population"`
	ControlSize    int     `json:"control_size"`
	TreatmentSize  int     `json:"treatment_size"`
	ControlValue   float64 `json:"control_value"`
	TreatmentValue float64 `json:"treatment_value"`
	Lift           float64 `json:"lift"`     // fractional, e.g. 0.1776
	CILower        float64 `json:"ci_lower"` // fractional lift
	CIUpper        float64 `json:"ci_upper"`
	Source         string  `json:"source"` // "simulated"
}

// ExperimentService runs simulated A/B experiments over the Merchant
// Simulator population.
type ExperimentService struct {
	db *pgxpool.Pool
}

func NewExperimentService(db *pgxpool.Pool) *ExperimentService {
	return &ExperimentService{db: db}
}

// Run simulates a controlled comparison: splits the Merchant Simulator
// population into control/treatment and computes revenue-per-session
// with a normal-approximation 95% CI for the lift.
func (s *ExperimentService) Run(
	ctx context.Context,
	name string,
	seed int64,
	revenuePerSession float64, // treatment multiplier applied to simulated sessions
) (ExperimentReport, error) {
	sessions := growth.NewMerchantSimulator(seed).Generate()

	total := len(sessions)
	half := total / 2

	var controlRevenue, treatmentRevenue float64

	for i, sess := range sessions {
		base := 0.0
		if sess.Purchased {
			// Revenue from the simulated dataset: the catalog price (paise)
			// of the purchased product, not a hardcoded order value.
			base = float64(sess.PurchaseAmount)
		}

		if i < half {
			controlRevenue += base
		} else {
			// Treatment: revenue multiplier (the "AI cross-sell" effect).
			treatmentRevenue += base * revenuePerSession
		}
	}

	controlMean := controlRevenue / float64(half)
	treatmentMean := treatmentRevenue / float64(half)

	// Lift and normal-approx 95% CI on the log-ratio of the means.
	lift := (treatmentMean - controlMean) / controlMean

	// Approximate CI: ±1.96 × sqrt((1/n_c) + (1/n_t)) on log scale.
	se := math.Sqrt(1.0/float64(half) + 1.0/float64(half))
	logLift := math.Log(1 + lift)
	ciLower := math.Exp(logLift-1.96*se) - 1
	ciUpper := math.Exp(logLift+1.96*se) - 1

	report := ExperimentReport{
		ID:             fmt.Sprintf("exp_%s", name),
		Name:           name,
		Metric:         "revenue_per_session",
		Population:     total,
		ControlSize:    half,
		TreatmentSize:  half,
		ControlValue:   controlMean,
		TreatmentValue: treatmentMean,
		Lift:           lift,
		CILower:        ciLower,
		CIUpper:        ciUpper,
		Source:         "simulated",
	}

	// Persist (upsert).
	_, err := s.db.Exec(ctx, `
		INSERT INTO experiments (
			id, name, metric, population, control_size, treatment_size,
			control_value, treatment_value, lift, ci_lower, ci_upper, source
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (id) DO UPDATE SET
			control_value = EXCLUDED.control_value,
			treatment_value = EXCLUDED.treatment_value,
			lift = EXCLUDED.lift,
			ci_lower = EXCLUDED.ci_lower,
			ci_upper = EXCLUDED.ci_upper
	`,
		report.ID, name, report.Metric, report.Population,
		report.ControlSize, report.TreatmentSize,
		report.ControlValue, report.TreatmentValue, report.Lift,
		report.CILower, report.CIUpper, report.Source,
	)
	if err != nil {
		return ExperimentReport{}, fmt.Errorf("save experiment: %w", err)
	}

	// Persist the control/treatment split per session (spec Phase 6 §2).
	// `i` is the unique session index — the simulator reuses customer IDs
	// across sessions, so session_id must be the loop index, not the
	// customer. Upsert so re-running on the same seed is idempotent.
	for i := 0; i < total; i++ {
		group := "treatment"
		if i < half {
			group = "control"
		}
		_, err := s.db.Exec(ctx, `
			INSERT INTO experiment_assignments (experiment_id, session_id, group_name)
			VALUES ($1, $2, $3)
			ON CONFLICT (experiment_id, session_id) DO UPDATE SET
				group_name = EXCLUDED.group_name
		`, report.ID, i, group)
		if err != nil {
			return ExperimentReport{}, fmt.Errorf("save experiment assignment: %w", err)
		}
	}

	return report, nil
}
