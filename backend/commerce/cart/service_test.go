package cart

import (
	"context"
	"testing"
	"time"

	"github.com/garinesaiajay/commerceos/commerce/catalog"
)

type fakeRepository struct {
	carts map[string]Cart
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		carts: make(map[string]Cart),
	}
}

func (r *fakeRepository) CreateCart(ctx context.Context, cart Cart) error {
	r.carts[cart.ID] = cart
	return nil
}

func (r *fakeRepository) GetCart(ctx context.Context, id string) (Cart, error) {
	cart, ok := r.carts[id]
	if !ok {
		return Cart{}, ErrCartNotFound
	}

	return cart, nil
}

func (r *fakeRepository) SaveCart(ctx context.Context, cart Cart) error {
	r.carts[cart.ID] = cart
	return nil
}

type fakeVariantReader struct {
	variants map[string]catalog.ProductVariant
}

func newFakeVariantReader() *fakeVariantReader {
	return &fakeVariantReader{
		variants: map[string]catalog.ProductVariant{
			"airpods-pro-2-default": {
				ID:        "airpods-pro-2-default",
				ProductID: "airpods-pro-2",
				SKU:       "AIRPODS-PRO-2",
				Price: catalog.Money{
					Amount:   24900,
					Currency: "INR",
				},
				Availability: 12,
				Attributes: map[string]any{
					"color": "white",
				},
			},
		},
	}
}

func (r *fakeVariantReader) GetVariant(
	ctx context.Context,
	id string,
) (catalog.ProductVariant, error) {
	variant, ok := r.variants[id]
	if !ok {
		return catalog.ProductVariant{}, catalog.ErrVariantNotFound
	}

	return variant, nil
}

func TestCreateCart(t *testing.T) {
	repo := newFakeRepository()
	variantReader := newFakeVariantReader()
	service := NewService(repo, variantReader)

	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)

	service.now = func() time.Time {
		return now
	}

	cart, err := service.CreateCart(
		context.Background(),
		"cart_001",
		"merchant_001",
		"INR",
	)
	if err != nil {
		t.Fatal(err)
	}

	if cart.ID != "cart_001" {
		t.Fatalf("expected cart ID cart_001, got %s", cart.ID)
	}

	if cart.ExpiresAt != now.Add(ReservationTTL) {
		t.Fatalf("unexpected expiration time: %v", cart.ExpiresAt)
	}

	if len(cart.Items) != 0 {
		t.Fatalf("expected empty cart, got %d items", len(cart.Items))
	}
}

func TestAddItemRecomputesTotal(t *testing.T) {
	repo := newFakeRepository()
	variantReader := newFakeVariantReader()
	service := NewService(repo, variantReader)

	_, err := service.CreateCart(
		context.Background(),
		"cart_001",
		"merchant_001",
		"INR",
	)
	if err != nil {
		t.Fatal(err)
	}

	item := CartItem{
		ProductID: "airpods-pro-2",
		VariantID: "airpods-pro-2-default",
		Title:     "AirPods Pro",
		Quantity:  2,
		UnitPrice: 24900,
	}

	err = service.AddItem(context.Background(), "cart_001", item)
	if err != nil {
		t.Fatal(err)
	}

	cart, err := service.GetCart(context.Background(), "cart_001")
	if err != nil {
		t.Fatal(err)
	}

	if cart.Subtotal != 49800 {
		t.Fatalf("expected subtotal 49800, got %d", cart.Subtotal)
	}

	if cart.Items[0].Total != 49800 {
		t.Fatalf("expected item total 49800, got %d", cart.Items[0].Total)
	}
}

func TestAddSameItemIncreasesQuantity(t *testing.T) {
	repo := newFakeRepository()
	variantReader := newFakeVariantReader()
	service := NewService(repo, variantReader)

	_, err := service.CreateCart(
		context.Background(),
		"cart_001",
		"merchant_001",
		"INR",
	)
	if err != nil {
		t.Fatal(err)
	}

	item := CartItem{
		ProductID: "airpods-pro-2",
		VariantID: "airpods-pro-2-default",
		Title:     "AirPods Pro",
		Quantity:  1,
		UnitPrice: 24900,
	}

	if err := service.AddItem(context.Background(), "cart_001", item); err != nil {
		t.Fatal(err)
	}

	if err := service.AddItem(context.Background(), "cart_001", item); err != nil {
		t.Fatal(err)
	}

	cart, err := service.GetCart(context.Background(), "cart_001")
	if err != nil {
		t.Fatal(err)
	}

	if len(cart.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(cart.Items))
	}

	if cart.Items[0].Quantity != 2 {
		t.Fatalf("expected quantity 2, got %d", cart.Items[0].Quantity)
	}

	if cart.Subtotal != 49800 {
		t.Fatalf("expected subtotal 49800, got %d", cart.Subtotal)
	}
}

func TestRemoveItem(t *testing.T) {
	repo := newFakeRepository()
	variantReader := newFakeVariantReader()
	service := NewService(repo, variantReader)

	_, err := service.CreateCart(
		context.Background(),
		"cart_001",
		"merchant_001",
		"INR",
	)
	if err != nil {
		t.Fatal(err)
	}

	item := CartItem{
		ProductID: "airpods-pro-2",
		VariantID: "airpods-pro-2-default",
		Title:     "AirPods Pro",
		Quantity:  1,
		UnitPrice: 24900,
	}

	if err := service.AddItem(context.Background(), "cart_001", item); err != nil {
		t.Fatal(err)
	}

	if err := service.RemoveItem(
		context.Background(),
		"cart_001",
		"airpods-pro-2-default",
	); err != nil {
		t.Fatal(err)
	}

	cart, err := service.GetCart(context.Background(), "cart_001")
	if err != nil {
		t.Fatal(err)
	}

	if len(cart.Items) != 0 {
		t.Fatalf("expected empty cart, got %d items", len(cart.Items))
	}

	if cart.Subtotal != 0 {
		t.Fatalf("expected subtotal 0, got %d", cart.Subtotal)
	}
}

func TestInvalidQuantity(t *testing.T) {
	repo := newFakeRepository()
	variantReader := newFakeVariantReader()
	service := NewService(repo, variantReader)

	_, err := service.CreateCart(
		context.Background(),
		"cart_001",
		"merchant_001",
		"INR",
	)
	if err != nil {
		t.Fatal(err)
	}

	err = service.AddItem(context.Background(), "cart_001", CartItem{
		VariantID: "airpods-pro-2-default",
		Quantity:  0,
		UnitPrice: 24900,
	})

	if err != ErrInvalidQuantity {
		t.Fatalf("expected ErrInvalidQuantity, got %v", err)
	}
}

func TestAddItemUsesCatalogPrice(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo, newFakeVariantReader())

	_, err := service.CreateCart(
		context.Background(),
		"cart_price_test",
		"merchant_001",
		"INR",
	)
	if err != nil {
		t.Fatal(err)
	}

	err = service.AddItem(context.Background(), "cart_price_test", CartItem{
		ProductID: "airpods-pro-2",
		VariantID: "airpods-pro-2-default",
		Title:     "AirPods Pro",
		Quantity:  1,
		UnitPrice: 1, // malicious/wrong client price
	})
	if err != nil {
		t.Fatal(err)
	}

	cart, err := service.GetCart(context.Background(), "cart_price_test")
	if err != nil {
		t.Fatal(err)
	}

	if cart.Items[0].UnitPrice != 24900 {
		t.Fatalf("expected catalog price 24900, got %d", cart.Items[0].UnitPrice)
	}

	if cart.Items[0].Total != 24900 {
		t.Fatalf("expected total 24900, got %d", cart.Items[0].Total)
	}
}

func TestAddItemRejectsInsufficientAvailability(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo, newFakeVariantReader())

	_, err := service.CreateCart(
		context.Background(),
		"cart_availability_test",
		"merchant_001",
		"INR",
	)
	if err != nil {
		t.Fatal(err)
	}

	err = service.AddItem(context.Background(), "cart_availability_test", CartItem{
		VariantID: "airpods-pro-2-default",
		Quantity:  13,
	})
	if err != ErrInsufficientAvailability {
		t.Fatalf(
			"expected ErrInsufficientAvailability, got %v",
			err,
		)
	}

	cart, err := service.GetCart(
		context.Background(),
		"cart_availability_test",
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(cart.Items) != 0 {
		t.Fatalf("expected cart to remain empty, got %d items", len(cart.Items))
	}
}

func TestAddItemRejectsCumulativeInsufficientAvailability(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo, newFakeVariantReader())

	_, err := service.CreateCart(
		context.Background(),
		"cart_cumulative_test",
		"merchant_001",
		"INR",
	)
	if err != nil {
		t.Fatal(err)
	}

	item := CartItem{
		VariantID: "airpods-pro-2-default",
		Quantity:  12,
	}

	if err := service.AddItem(
		context.Background(),
		"cart_cumulative_test",
		item,
	); err != nil {
		t.Fatal(err)
	}

	err = service.AddItem(
		context.Background(),
		"cart_cumulative_test",
		CartItem{
			VariantID: "airpods-pro-2-default",
			Quantity:  1,
		},
	)
	if err != ErrInsufficientAvailability {
		t.Fatalf(
			"expected ErrInsufficientAvailability, got %v",
			err,
		)
	}

	cart, err := service.GetCart(
		context.Background(),
		"cart_cumulative_test",
	)
	if err != nil {
		t.Fatal(err)
	}

	if len(cart.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(cart.Items))
	}

	if cart.Items[0].Quantity != 12 {
		t.Fatalf(
			"expected quantity to remain 12, got %d",
			cart.Items[0].Quantity,
		)
	}
}

func TestAddItemRejectsUnknownVariant(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo, newFakeVariantReader())

	_, err := service.CreateCart(
		context.Background(),
		"cart_variant_test",
		"merchant_001",
		"INR",
	)
	if err != nil {
		t.Fatal(err)
	}

	err = service.AddItem(context.Background(), "cart_variant_test", CartItem{
		VariantID: "does-not-exist",
		Quantity:  1,
	})
	if err != catalog.ErrVariantNotFound {
		t.Fatalf(
			"expected catalog.ErrVariantNotFound, got %v",
			err,
		)
	}
}

func TestUpdateItemQuantity(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo, newFakeVariantReader())

	_, err := service.CreateCart(
		context.Background(),
		"cart_update_test",
		"merchant_001",
		"INR",
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := service.AddItem(
		context.Background(),
		"cart_update_test",
		CartItem{
			VariantID: "airpods-pro-2-default",
			Quantity:  2,
		},
	); err != nil {
		t.Fatal(err)
	}

	if err := service.UpdateItemQuantity(
		context.Background(),
		"cart_update_test",
		"airpods-pro-2-default",
		5,
	); err != nil {
		t.Fatal(err)
	}

	cart, err := service.GetCart(
		context.Background(),
		"cart_update_test",
	)
	if err != nil {
		t.Fatal(err)
	}

	if cart.Items[0].Quantity != 5 {
		t.Fatalf("expected quantity 5, got %d", cart.Items[0].Quantity)
	}

	if cart.Items[0].Total != 124500 {
		t.Fatalf("expected total 124500, got %d", cart.Items[0].Total)
	}

	if cart.Subtotal != 124500 {
		t.Fatalf("expected subtotal 124500, got %d", cart.Subtotal)
	}
}

func TestUpdateItemQuantityRejectsInsufficientAvailability(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo, newFakeVariantReader())

	_, err := service.CreateCart(
		context.Background(),
		"cart_update_availability_test",
		"merchant_001",
		"INR",
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := service.AddItem(
		context.Background(),
		"cart_update_availability_test",
		CartItem{
			VariantID: "airpods-pro-2-default",
			Quantity:  2,
		},
	); err != nil {
		t.Fatal(err)
	}

	err = service.UpdateItemQuantity(
		context.Background(),
		"cart_update_availability_test",
		"airpods-pro-2-default",
		13,
	)
	if err != ErrInsufficientAvailability {
		t.Fatalf(
			"expected ErrInsufficientAvailability, got %v",
			err,
		)
	}

	cart, err := service.GetCart(
		context.Background(),
		"cart_update_availability_test",
	)
	if err != nil {
		t.Fatal(err)
	}

	if cart.Items[0].Quantity != 2 {
		t.Fatalf(
			"expected quantity to remain 2, got %d",
			cart.Items[0].Quantity,
		)
	}
}

func TestUpdateItemQuantityRejectsInvalidQuantity(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo, newFakeVariantReader())

	err := service.UpdateItemQuantity(
		context.Background(),
		"cart_update_test",
		"airpods-pro-2-default",
		0,
	)
	if err != ErrInvalidQuantity {
		t.Fatalf(
			"expected ErrInvalidQuantity, got %v",
			err,
		)
	}
}

func TestUpdateItemQuantityRejectsMissingItem(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo, newFakeVariantReader())

	_, err := service.CreateCart(
		context.Background(),
		"cart_update_missing_test",
		"merchant_001",
		"INR",
	)
	if err != nil {
		t.Fatal(err)
	}

	err = service.UpdateItemQuantity(
		context.Background(),
		"cart_update_missing_test",
		"airpods-pro-2-default",
		5,
	)
	if err != ErrItemNotFound {
		t.Fatalf(
			"expected ErrItemNotFound, got %v",
			err,
		)
	}
}
