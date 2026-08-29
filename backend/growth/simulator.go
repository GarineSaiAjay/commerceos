package growth

import (
	"fmt"
	"math/rand"
)

// MerchantSimulator generates a reproducible synthetic dataset with a
// fixed seed — 10,000 customers, 50,000 sessions, purchases, clicks,
// cart additions, abandoned carts, returns. Feeds Phase 6 experiments.
//
// There is deliberately no standalone CLI/generator script wrapping this --
// the dataset is regenerated in-process, on demand, by whatever calls
// NewMerchantSimulator(seed).Generate(products) (see analytics/experiment.go),
// which keeps the seed and the consuming code from drifting apart. A
// separate script would just be a thinner, duplicate way to call the
// same lines.
//
// Generate previously drew from a hardcoded 4-SKU list/price map that
// only covered 4 of the 10 products actually seeded into the catalog
// (db/seeds/001_catalog.sql), so every simulated A/B experiment and
// growth-segment description silently ignored 6 of the 10 real SKUs.
// Generate now takes the product list as a parameter -- the caller is
// responsible for sourcing it from the real catalog (experiment.go
// queries the products table) -- so the simulator can never drift out
// of sync with whatever is actually seeded.
type MerchantSimulator struct {
	seed int64
}

func NewMerchantSimulator(seed int64) *MerchantSimulator {
	return &MerchantSimulator{seed: seed}
}

// ProductInfo is the minimal catalog data the simulator needs to
// generate realistic sessions: a product ID to assign to a session,
// and its price (paise) to compute PurchaseAmount when purchased.
type ProductInfo struct {
	ID          string
	PriceAmount int64
}

// Session is one synthetic customer session.
type Session struct {
	CustomerID    int
	ProductID     string
	Clicked       bool
	AddedToCart   bool
	Purchased     bool
	AbandonedCart bool
	Returned      bool
	// PurchaseAmount is the catalog price in paise of the purchased
	// product, set when Purchased is true. It lets the experimentation
	// engine compute revenue-per-session from the simulated dataset
	// instead of a hardcoded order value.
	PurchaseAmount int64
}

// Generate produces the dataset deterministically, drawing product IDs
// (and their prices, for PurchaseAmount) from the supplied products
// list. Returns nil if products is empty -- there is nothing sensible
// to generate a session about otherwise, and callers should treat that
// as a configuration error rather than let rng.Intn(0) panic.
func (m *MerchantSimulator) Generate(products []ProductInfo) []Session {
	if len(products) == 0 {
		return nil
	}

	rng := rand.New(rand.NewSource(m.seed))

	sessions := make([]Session, 0, 50000)

	for i := 0; i < 50000; i++ {
		product := products[rng.Intn(len(products))]
		s := Session{
			CustomerID:     i % 10000,
			ProductID:      product.ID,
			Clicked:        rng.Float64() < 0.6,
			AddedToCart:    rng.Float64() < 0.3,
			Purchased:      rng.Float64() < 0.15,
			PurchaseAmount: product.PriceAmount,
		}
		s.AbandonedCart = s.AddedToCart && !s.Purchased
		s.Returned = s.Purchased && rng.Float64() < 0.03

		sessions = append(sessions, s)
	}

	return sessions
}

// PurchaseRate returns P(purchase) for a product from the dataset.
func PurchaseRate(sessions []Session, productID string) float64 {
	var views, purchases int

	for _, s := range sessions {
		if s.ProductID == productID {
			views++
			if s.Purchased {
				purchases++
			}
		}
	}

	if views == 0 {
		return 0
	}

	return float64(purchases) / float64(views)
}

// Describe prints a segment-level summary (e.g. budget-conscious
// students) for the demo. "airpods-case" is one of the real seeded
// catalog IDs (db/seeds/001_catalog.sql), so this stays meaningful
// regardless of which products the caller passed to Generate.
func Describe(sessions []Session) string {
	rate := PurchaseRate(sessions, "airpods-case")
	return fmt.Sprintf(
		"Segment: budget-conscious buyers — P(purchase|airpods-case)=%.2f, high-probability bundle: earbuds+case, low-probability: premium accessories",
		rate,
	)
}
