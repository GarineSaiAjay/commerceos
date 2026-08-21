package catalog

import (
	"encoding/json"
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
