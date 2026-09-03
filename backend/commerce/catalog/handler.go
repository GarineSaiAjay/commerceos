package catalog

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

func (h *Handler) ListProducts(w http.ResponseWriter, r *http.Request) {
	products, err := h.service.ListProducts(r.Context())
	if err != nil {
		http.Error(w, "failed to list products", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(products); err != nil {
		http.Error(w, "failed to encode products", http.StatusInternalServerError)
	}
}

func (h *Handler) GetProduct(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/products/")

	if id == "" {
		http.Error(w, "product ID is required", http.StatusBadRequest)
		return
	}

	product, err := h.service.GetProduct(r.Context(), id)
	if err == ErrProductNotFound {
		http.Error(w, "product not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "failed to get product", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(product); err != nil {
		http.Error(w, "failed to encode product", http.StatusInternalServerError)
	}
}

// validateProduct checks the fields a merchant is required to supply when
// creating or updating a catalog listing. It intentionally does not
// validate Features/Compatibility/UseCases/Attributes/PurchaseConstraints
// contents -- those are free-form by design (see Product).
func validateProduct(product Product) string {
	if strings.TrimSpace(product.Title) == "" {
		return "title is required"
	}
	if strings.TrimSpace(product.Merchant.ID) == "" {
		return "merchant.id is required"
	}
	if product.Price.Amount < 0 {
		return "price.amount must not be negative"
	}
	if len(product.Price.Currency) != 3 {
		return "price.currency must be a 3-letter ISO code"
	}
	if product.Availability < 0 {
		return "availability must not be negative"
	}
	return ""
}

// CreateProduct handles POST /products. The request body is decoded
// directly into a catalog.Product -- its JSON tags already match the
// wire shape used everywhere else (product_id, price:{amount,currency},
// merchant:{id}, etc).
func (h *Handler) CreateProduct(w http.ResponseWriter, r *http.Request) {
	var product Product
	if err := json.NewDecoder(r.Body).Decode(&product); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(product.ID) == "" {
		http.Error(w, "product_id is required", http.StatusBadRequest)
		return
	}
	if msg := validateProduct(product); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	err := h.service.CreateProduct(r.Context(), product)
	if errors.Is(err, ErrProductAlreadyExists) {
		http.Error(w, "product already exists", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "failed to create product", http.StatusInternalServerError)
		return
	}

	created, err := h.service.GetProduct(r.Context(), product.ID)
	if err != nil {
		// The write succeeded; a failed read-back shouldn't be reported
		// as a create failure. Fall back to echoing what was sent.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(product)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(created); err != nil {
		http.Error(w, "failed to encode product", http.StatusInternalServerError)
	}
}

// UpdateProduct handles PATCH /products/{id}. The URL path is authoritative
// for the product ID, not whatever the body claims -- callers cannot
// rename a product by editing product_id in the payload.
func (h *Handler) UpdateProduct(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/products/")
	if id == "" {
		http.Error(w, "product ID is required", http.StatusBadRequest)
		return
	}

	var product Product
	if err := json.NewDecoder(r.Body).Decode(&product); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	product.ID = id

	if msg := validateProduct(product); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	err := h.service.UpdateProduct(r.Context(), product)
	if errors.Is(err, ErrProductNotFound) {
		http.Error(w, "product not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "failed to update product", http.StatusInternalServerError)
		return
	}

	updated, err := h.service.GetProduct(r.Context(), id)
	if err != nil {
		http.Error(w, "failed to load updated product", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(updated); err != nil {
		http.Error(w, "failed to encode product", http.StatusInternalServerError)
	}
}

// DeleteProduct handles DELETE /products/{id}.
func (h *Handler) DeleteProduct(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/products/")
	if id == "" {
		http.Error(w, "product ID is required", http.StatusBadRequest)
		return
	}

	err := h.service.DeleteProduct(r.Context(), id)
	if errors.Is(err, ErrProductNotFound) {
		http.Error(w, "product not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, ErrProductInUse) {
		http.Error(w, "product is referenced by an existing cart or order and cannot be deleted", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "failed to delete product", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetVariant(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/variants/")

	if id == "" {
		http.Error(w, "variant ID is required", http.StatusBadRequest)
		return
	}

	variant, err := h.service.GetVariant(r.Context(), id)
	if err == ErrVariantNotFound {
		http.Error(w, "variant not found", http.StatusNotFound)
		return
	}

	if err != nil {
		http.Error(w, "failed to get variant", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(variant); err != nil {
		http.Error(w, "failed to encode variant", http.StatusInternalServerError)
	}
}

// validateVariant mirrors validateProduct's shape for the fields a
// variant sub-editor (PLAN-02-CATALOG-AND-COMMERCE.md §5.2 / PLAN-05-
// SELLER-DASHBOARD.md §1) can set. attributes is free-form by design,
// same as Product's, so it's never validated here.
func validateVariant(variant ProductVariant) string {
	if strings.TrimSpace(variant.SKU) == "" {
		return "sku is required"
	}
	if variant.Price.Amount < 0 {
		return "price.amount must not be negative"
	}
	if variant.Availability < 0 {
		return "availability must not be negative"
	}
	return ""
}

// ListVariants handles GET /products/{id}/variants -- exposed
// separately from GetProduct's own embedded Variants field mainly so a
// variant sub-editor can refresh just the variant list after its own
// mutation without refetching (and re-triggering every downstream
// effect keyed off) the whole product.
func (h *Handler) ListVariants(w http.ResponseWriter, r *http.Request) {
	productID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/products/"), "/variants")
	if productID == "" {
		http.Error(w, "product ID is required", http.StatusBadRequest)
		return
	}

	variants, err := h.service.ListVariantsByProduct(r.Context(), productID)
	if err != nil {
		http.Error(w, "failed to list variants", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(variants); err != nil {
		http.Error(w, "failed to encode variants", http.StatusInternalServerError)
	}
}

// CreateVariant handles POST /products/{id}/variants. The URL path is
// authoritative for product_id, not whatever the body claims -- mirrors
// UpdateProduct's own path-is-authoritative convention. price.currency
// in the request body is ignored: product_variants has no currency
// column of its own, a variant's currency is always its parent
// product's (see GetVariant/ListVariantsByProduct's own JOIN).
func (h *Handler) CreateVariant(w http.ResponseWriter, r *http.Request) {
	productID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/products/"), "/variants")
	if productID == "" {
		http.Error(w, "product ID is required", http.StatusBadRequest)
		return
	}

	var variant ProductVariant
	if err := json.NewDecoder(r.Body).Decode(&variant); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	variant.ProductID = productID

	if strings.TrimSpace(variant.ID) == "" {
		http.Error(w, "variant_id is required", http.StatusBadRequest)
		return
	}
	if msg := validateVariant(variant); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	err := h.service.CreateVariant(r.Context(), variant)
	if errors.Is(err, ErrProductNotFound) {
		http.Error(w, "product not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, ErrVariantAlreadyExists) {
		http.Error(w, "variant already exists", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "failed to create variant", http.StatusInternalServerError)
		return
	}

	created, err := h.service.GetVariant(r.Context(), variant.ID)
	if err != nil {
		// The write succeeded; a failed read-back shouldn't be reported
		// as a create failure. Fall back to echoing what was sent.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(variant)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(created); err != nil {
		http.Error(w, "failed to encode variant", http.StatusInternalServerError)
	}
}

// UpdateVariant handles PATCH /variants/{id}. The URL path is
// authoritative for the variant ID; product_id cannot be changed
// through this endpoint (a variant can't be moved to a different
// product) even if the body includes one.
func (h *Handler) UpdateVariant(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/variants/")
	if id == "" {
		http.Error(w, "variant ID is required", http.StatusBadRequest)
		return
	}

	var variant ProductVariant
	if err := json.NewDecoder(r.Body).Decode(&variant); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	variant.ID = id

	if msg := validateVariant(variant); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	err := h.service.UpdateVariant(r.Context(), variant)
	if errors.Is(err, ErrVariantNotFound) {
		http.Error(w, "variant not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, ErrVariantAlreadyExists) {
		http.Error(w, "variant already exists", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "failed to update variant", http.StatusInternalServerError)
		return
	}

	updated, err := h.service.GetVariant(r.Context(), id)
	if err != nil {
		http.Error(w, "failed to load updated variant", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(updated); err != nil {
		http.Error(w, "failed to encode variant", http.StatusInternalServerError)
	}
}

// DeleteVariant handles DELETE /variants/{id}. Deliberately does NOT
// refuse deleting a product's last remaining variant -- checkout.tsx's
// addToCart and the MCP add_item tool both assume every real product
// has at least one variant, so a merchant who deletes it will break
// "Add to cart" for that product until they add a new one. That's a
// real, sharp edge, left as an explicit trade-off rather than a
// backend-enforced guard (frontend/app/dashboard/catalog/
// VariantEditor.tsx warns about it inline instead): a merchant might
// legitimately want to delete a stale placeholder variant and add a
// real one in the same edit session, and a hard block would only get
// in the way of that.
func (h *Handler) DeleteVariant(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/variants/")
	if id == "" {
		http.Error(w, "variant ID is required", http.StatusBadRequest)
		return
	}

	err := h.service.DeleteVariant(r.Context(), id)
	if errors.Is(err, ErrVariantNotFound) {
		http.Error(w, "variant not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, ErrVariantInUse) {
		http.Error(w, "variant is referenced by an existing cart or order and cannot be deleted", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "failed to delete variant", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
