package cart

import (
	"context"
	"errors"
	"time"

	"github.com/garinesaiajay/commerceos/commerce/catalog"
)

const ReservationTTL = 9 * time.Minute

var ErrCartNotFound = errors.New("cart not found")
var ErrInvalidQuantity = errors.New("invalid quantity")
var ErrItemNotFound = errors.New("cart item not found")
var ErrInsufficientAvailability = errors.New("insufficient availability")

type VariantReader interface {
	GetVariant(ctx context.Context, id string) (catalog.ProductVariant, error)
}

type Service struct {
	repo          Repository
	variantReader VariantReader
	now           func() time.Time
}

func NewService(repo Repository, variantReader VariantReader) *Service {
	return &Service{
		repo:          repo,
		variantReader: variantReader,
		now:           time.Now,
	}
}

func (s *Service) CreateCart(
	ctx context.Context,
	id string,
	merchantID string,
	currency string,
) (Cart, error) {
	cart := Cart{
		ID:         id,
		MerchantID: merchantID,
		Currency:   currency,
		Items:      []CartItem{},
		ExpiresAt:  s.now().Add(ReservationTTL),
	}

	if err := s.repo.CreateCart(ctx, cart); err != nil {
		return Cart{}, err
	}

	return cart, nil
}

func (s *Service) AddItem(
	ctx context.Context,
	cartID string,
	item CartItem,
) error {
	if item.Quantity <= 0 {
		return ErrInvalidQuantity
	}

	cart, err := s.repo.GetCart(ctx, cartID)
	if err != nil {
		return err
	}

	variant, err := s.variantReader.GetVariant(ctx, item.VariantID)
	if err != nil {
		return err
	}

	// The catalog is the source of truth for product information.
	item.ProductID = variant.ProductID
	item.UnitPrice = variant.Price.Amount

	for i := range cart.Items {
		if cart.Items[i].VariantID == item.VariantID {
			newQuantity := cart.Items[i].Quantity + item.Quantity

			if newQuantity > variant.Availability {
				return ErrInsufficientAvailability
			}

			cart.Items[i].Quantity = newQuantity
			cart.Items[i].UnitPrice = variant.Price.Amount
			cart.Items[i].Total =
				cart.Items[i].UnitPrice * int64(cart.Items[i].Quantity)

			s.recomputeTotal(&cart)

			return s.repo.SaveCart(ctx, cart)
		}
	}

	if item.Quantity > variant.Availability {
		return ErrInsufficientAvailability
	}

	item.Total = item.UnitPrice * int64(item.Quantity)

	cart.Items = append(cart.Items, item)

	s.recomputeTotal(&cart)

	return s.repo.SaveCart(ctx, cart)
}

func (s *Service) UpdateItemQuantity(
	ctx context.Context,
	cartID string,
	variantID string,
	quantity int,
) error {
	if quantity <= 0 {
		return ErrInvalidQuantity
	}

	cart, err := s.repo.GetCart(ctx, cartID)
	if err != nil {
		return err
	}

	variant, err := s.variantReader.GetVariant(ctx, variantID)
	if err != nil {
		return err
	}

	if quantity > variant.Availability {
		return ErrInsufficientAvailability
	}

	for i := range cart.Items {
		if cart.Items[i].VariantID == variantID {
			cart.Items[i].Quantity = quantity
			cart.Items[i].UnitPrice = variant.Price.Amount

			s.recomputeTotal(&cart)

			return s.repo.SaveCart(ctx, cart)
		}
	}

	return ErrItemNotFound
}

func (s *Service) RemoveItem(
	ctx context.Context,
	cartID string,
	variantID string,
) error {
	cart, err := s.repo.GetCart(ctx, cartID)
	if err != nil {
		return err
	}

	for i := range cart.Items {
		if cart.Items[i].VariantID == variantID {
			cart.Items = append(
				cart.Items[:i],
				cart.Items[i+1:]...,
			)

			s.recomputeTotal(&cart)

			return s.repo.SaveCart(ctx, cart)
		}
	}

	return ErrItemNotFound
}

func (s *Service) GetCart(
	ctx context.Context,
	cartID string,
) (Cart, error) {
	cart, err := s.repo.GetCart(ctx, cartID)
	if err != nil {
		return Cart{}, err
	}

	// A checked-out cart has already become an order (see
	// order/postgres_repository.go CheckoutCart) and is single-use --
	// treat it the same as a cart that was never created so callers
	// (including the frontend's reload-restore check) never resume
	// shopping into an already-purchased cart.
	if cart.Status == "checked_out" {
		return Cart{}, ErrCartNotFound
	}

	if s.now().After(cart.ExpiresAt) {
		return Cart{}, errors.New("cart reservation expired")
	}

	return cart, nil
}

func (s *Service) recomputeTotal(cart *Cart) {
	var subtotal int64

	for i := range cart.Items {
		cart.Items[i].Total =
			cart.Items[i].UnitPrice * int64(cart.Items[i].Quantity)

		subtotal += cart.Items[i].Total
	}

	cart.Subtotal = subtotal
}
