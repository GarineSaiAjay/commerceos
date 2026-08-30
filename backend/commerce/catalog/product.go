package catalog

type Money struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type Product struct {
	ID                  string         `json:"product_id"`
	Title               string         `json:"title"`
	Price               Money          `json:"price"`
	Availability        int            `json:"availability"`
	Features            []string       `json:"features"`
	Compatibility       []string       `json:"compatibility"`
	UseCases            []string       `json:"use_cases"`
	Merchant            MerchantRef    `json:"merchant"`
	ReturnPolicy        ReturnPolicy   `json:"return_policy"`
	Shipping            Shipping       `json:"shipping"`
	Attributes          map[string]any `json:"attributes"`
	PurchaseConstraints map[string]any `json:"purchase_constraints"`
	// AverageRating and ReviewCount (PLAN-02-CATALOG-AND-COMMERCE.md
	// §2) are computed at read time from the reviews table -- never
	// stored on products itself, so they can never go stale the way a
	// denormalized counter could. AverageRating is 0 with ReviewCount 0
	// for a product with no reviews yet, never null -- "no reviews" and
	// "reviewed 0 stars" must stay visibly distinct in the UI, which is
	// why ReviewCount, not just a zero AverageRating, is always present.
	// Repository.CreateProduct/UpdateProduct never write these fields --
	// they exist only on the read path (PostgresRepository.GetProduct's
	// join), so a caller POSTing average_rating on /products has no
	// effect.
	AverageRating float64 `json:"average_rating"`
	ReviewCount   int     `json:"review_count"`
}

type ProductVariant struct {
	ID           string         `json:"variant_id"`
	ProductID    string         `json:"product_id"`
	SKU          string         `json:"sku"`
	Price        Money          `json:"price"`
	Availability int            `json:"availability"`
	Attributes   map[string]any `json:"attributes"`
}

type MerchantRef struct {
	ID string `json:"id"`
}

type ReturnPolicy struct {
	Days int `json:"days"`
}

type Shipping struct {
	EstimatedDays int `json:"estimated_days"`
}
