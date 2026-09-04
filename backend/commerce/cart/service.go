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

// ErrCartConflict is returned by Repository.SaveCart when the cart was
// modified by someone else since it was last read (full-codebase
// re-audit, P2 -- see 20260904110000_add_carts_version.sql). mutateCart
// below retries the whole read-modify-write on this error rather than
// propagating it to most callers; it is exported because it can still
// surface if maxCartMutateRetries is exhausted under sustained
// contention.
var ErrCartConflict = errors.New("cart was concurrently modified, please retry")

// maxCartMutateRetries bounds mutateCart's retry loop. A real conflict
// only ever needs a handful of retries to resolve (each one is one
// buyer's own rapid double-click or one agent/UI race, not sustained
// contention from many concurrent writers on the same cart), so this is
// generous headroom against a genuinely stuck scenario without looping
// forever.
const maxCartMutateRetries = 5

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

// mutateCart implements the retry-on-conflict read-modify-write shared
// by AddItem/UpdateItemQuantity/RemoveItem (full-codebase re-audit,
// P2): it re-reads the cart, hands it to mutate, and saves the result
// mutate returns -- retrying the whole cycle from a fresh read whenever
// Repository.SaveCart reports ErrCartConflict (another writer saved
// this cart first), instead of the previous single unlocked
// GetCart-then-SaveCart that let a concurrent writer's update silently
// disappear. mutate returns the cart to save, or an error (e.g.
// ErrInsufficientAvailability, ErrItemNotFound) that aborts immediately
// without ever calling SaveCart -- matching every existing caller's
// "no-op on a rejected mutation" expectation.
//
// This intentionally calls s.repo.GetCart directly, not s.GetCart --
// same as every call site it replaces did before this fix -- so it
// does not change whether an expired or already-checked-out cart can
// still be mutated; that's a separate question from the lost-update
// race this fixes.
func (s *Service) mutateCart(
	ctx context.Context,
	cartID string,
	mutate func(cart Cart) (Cart, error),
) error {
	var lastErr error

	for attempt := 0; attempt < maxCartMutateRetries; attempt++ {
		cart, err := s.repo.GetCart(ctx, cartID)
		if err != nil {
			return err
		}

		updated, err := mutate(cart)
		if err != nil {
			return err
		}

		err = s.repo.SaveCart(ctx, updated)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrCartConflict) {
			return err
		}
		lastErr = err
	}

	return lastErr
}

func (s *Service) AddItem(
	ctx context.Context,
	cartID string,
	item CartItem,
) error {
	if item.Quantity <= 0 {
		return ErrInvalidQuantity
	}

	variant, err := s.variantReader.GetVariant(ctx, item.VariantID)
	if err != nil {
		return err
	}

	// The catalog is the source of truth for product information.
	item.ProductID = variant.ProductID
	item.UnitPrice = variant.Price.Amount

	return s.mutateCart(ctx, cartID, func(cart Cart) (Cart, error) {
		for i := range cart.Items {
			if cart.Items[i].VariantID == item.VariantID {
				newQuantity := cart.Items[i].Quantity + item.Quantity

				if newQuantity > variant.Availability {
					return Cart{}, ErrInsufficientAvailability
				}

				cart.Items[i].Quantity = newQuantity
				cart.Items[i].UnitPrice = variant.Price.Amount
				cart.Items[i].Total =
					cart.Items[i].UnitPrice * int64(cart.Items[i].Quantity)

				s.recomputeTotal(&cart)

				return cart, nil
			}
		}

		if item.Quantity > variant.Availability {
			return Cart{}, ErrInsufficientAvailability
		}

		item.Total = item.UnitPrice * int64(item.Quantity)

		cart.Items = append(cart.Items, item)

		s.recomputeTotal(&cart)

		return cart, nil
	})
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

	variant, err := s.variantReader.GetVariant(ctx, variantID)
	if err != nil {
		return err
	}

	if quantity > variant.Availability {
		return ErrInsufficientAvailability
	}

	return s.mutateCart(ctx, cartID, func(cart Cart) (Cart, error) {
		for i := range cart.Items {
			if cart.Items[i].VariantID == variantID {
				cart.Items[i].Quantity = quantity
				cart.Items[i].UnitPrice = variant.Price.Amount

				s.recomputeTotal(&cart)

				return cart, nil
			}
		}

		return Cart{}, ErrItemNotFound
	})
}

func (s *Service) RemoveItem(
	ctx context.Context,
	cartID string,
	variantID string,
) error {
	return s.mutateCart(ctx, cartID, func(cart Cart) (Cart, error) {
		for i := range cart.Items {
			if cart.Items[i].VariantID == variantID {
				cart.Items = append(
					cart.Items[:i],
					cart.Items[i+1:]...,
				)

				s.recomputeTotal(&cart)

				return cart, nil
			}
		}

		return Cart{}, ErrItemNotFound
	})
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
