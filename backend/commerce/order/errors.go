package order

import "errors"

var (
	ErrOrderNotFound            = errors.New("order not found")
	ErrCartNotFound             = errors.New("cart not found")
	ErrCartExpired              = errors.New("cart reservation expired")
	ErrCartEmpty                = errors.New("cart is empty")
	ErrCartAlreadyCheckedOut    = errors.New("cart already checked out")
	ErrInsufficientAvailability = errors.New("insufficient availability")
)
