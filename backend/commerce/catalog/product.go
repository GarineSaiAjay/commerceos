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
