-- +goose Up
-- Earlier seed data used rupees while Razorpay and the UI use paise.
UPDATE products SET price_amount = price_amount * 100;
UPDATE product_variants SET price_amount = price_amount * 100;
UPDATE carts SET subtotal_amount = subtotal_amount * 100;
UPDATE cart_items SET unit_price_amount = unit_price_amount * 100, total_amount = total_amount * 100;
UPDATE orders SET subtotal = subtotal * 100;
UPDATE order_items SET unit_price = unit_price * 100, total = total * 100;
UPDATE payments SET amount = amount * 100;
UPDATE payment_attempts SET amount = amount * 100;
UPDATE mandates SET maximum_amount = maximum_amount * 100, requires_confirmation_above = requires_confirmation_above * 100;
UPDATE authorizations SET amount = amount * 100;
UPDATE agent_actions SET amount = amount * 100;

-- +goose Down
UPDATE products SET price_amount = price_amount / 100;
UPDATE product_variants SET price_amount = price_amount / 100;
UPDATE carts SET subtotal_amount = subtotal_amount / 100;
UPDATE cart_items SET unit_price_amount = unit_price_amount / 100, total_amount = total_amount / 100;
UPDATE orders SET subtotal = subtotal / 100;
UPDATE order_items SET unit_price = unit_price / 100, total = total / 100;
UPDATE payments SET amount = amount / 100;
UPDATE payment_attempts SET amount = amount / 100;
UPDATE mandates SET maximum_amount = maximum_amount / 100, requires_confirmation_above = requires_confirmation_above / 100;
UPDATE authorizations SET amount = amount / 100;
UPDATE agent_actions SET amount = amount / 100;
