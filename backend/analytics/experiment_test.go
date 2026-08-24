package analytics

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestExperimentReproducible proves spec: running the experiment twice
// on the same seed produces the same lift and CI — a real calculation.
func TestExperimentReproducible(t *testing.T) {
	ctx := context.Background()

	pool, err := pgxpool.New(
		ctx,
		"postgres://commerceos:commerceos_dev_password@localhost:5433/commerceos?sslmode=disable",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	s := NewExperimentService(pool)

	first, err := s.Run(ctx, "test_rep", 42, 1.5)
	if err != nil {
		t.Fatal(err)
	}

	second, err := s.Run(ctx, "test_rep", 42, 1.5)
	if err != nil {
		t.Fatal(err)
	}

	if first.Lift != second.Lift {
		t.Fatalf("lift differs: %v vs %v", first.Lift, second.Lift)
	}
	if first.CILower != second.CILower || first.CIUpper != second.CIUpper {
		t.Fatalf("CI differs: %v-%v vs %v-%v",
			first.CILower, first.CIUpper, second.CILower, second.CIUpper)
	}

	if first.Source != "simulated" {
		t.Fatalf("expected simulated source, got %s", first.Source)
	}

	// Sanity: lift should be positive when treatment > 1.
	if first.Lift <= 0 {
		t.Fatalf("expected positive lift for >1 treatment, got %v", first.Lift)
	}
}
