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
