package growth

import (
	"fmt"
	"math/rand"
)

// MerchantSimulator generates a reproducible synthetic dataset with a
// fixed seed — 10,000 customers, 50,000 sessions, purchases, clicks,
// cart additions, abandoned carts, returns. Feeds Phase 6 experiments.
type MerchantSimulator struct {
	seed int64
}

func NewMerchantSimulator(seed int64) *MerchantSimulator {
	return &MerchantSimulator{seed: seed}
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

// ProductPrices maps the seeded catalog SKUs to their paise amounts so
// the generated dataset is consistent with the real catalog.
var ProductPrices = map[string]int64{
	"airpods-pro-2": 2_490_000, // ₹24,900
	"airpods-case":  199_900,   // ₹1,999
	"applecare":     250_000,   // ₹2,500
	"usb-c-adapter": 129_900,   // ₹1,299
}

// Generate produces the dataset deterministically.
func (m *MerchantSimulator) Generate() []Session {
	rng := rand.New(rand.NewSource(m.seed))

	products := []string{
		"airpods-pro-2", "airpods-case", "applecare", "usb-c-adapter",
	}

	sessions := make([]Session, 0, 50000)

	for i := 0; i < 50000; i++ {
		productID := products[rng.Intn(len(products))]
		s := Session{
			CustomerID:     i % 10000,
			ProductID:      productID,
			Clicked:        rng.Float64() < 0.6,
			AddedToCart:    rng.Float64() < 0.3,
			Purchased:      rng.Float64() < 0.15,
			PurchaseAmount: ProductPrices[productID],
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
// students) for the demo.
func Describe(sessions []Session) string {
	rate := PurchaseRate(sessions, "airpods-case")
	return fmt.Sprintf(
		"Segment: budget-conscious buyers — P(purchase|airpods-case)=%.2f, high-probability bundle: earbuds+case, low-probability: premium accessories",
		rate,
	)
}
