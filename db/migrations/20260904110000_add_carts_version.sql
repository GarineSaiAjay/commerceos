-- +goose Up

-- Full-codebase re-audit (P2): cart.Service.AddItem/UpdateItemQuantity/
-- RemoveItem each do a classic read-modify-write -- GetCart, mutate the
-- in-memory struct, SaveCart -- as two separate, unlocked repository
-- calls with no transaction spanning them (backend/commerce/cart/
-- service.go). Two concurrent mutations against the same cart (a
-- double-click on "Add to cart", or the shopping agent and the buyer's
-- own UI racing) can both read the same starting state and independently
-- SaveCart their own result; the second write silently clobbers the
-- first's addition, a classic lost update.
--
-- Fixed via optimistic concurrency rather than a row lock spanning the
-- read and the write: cart.Service's read (GetCart) and write (SaveCart)
-- are two separate Repository calls with Go-level business logic
-- (catalog availability checks) between them, so a lock acquired during
-- the read would need to be held across a network round-trip back into
-- application code to still protect the write -- awkward and easy to
-- get wrong compared to a version check on the write itself.
--
-- version increments by one on every successful SaveCart
-- (postgres_repository.go); SaveCart's UPDATE is now conditioned on
-- `WHERE id = $1 AND version = $2` using the version the caller last
-- read, and cart.Service retries the whole read-modify-write (bounded)
-- when zero rows are affected -- the same "conditional UPDATE + rows-
-- affected check" shape as MarkAuthorizationUsed's atomic consume (P0
-- fix, full-codebase re-audit 2026-09-04), applied here to a read-then-
-- write span instead of a single statement.
ALTER TABLE carts ADD COLUMN version INTEGER NOT NULL DEFAULT 0;

-- +goose Down

ALTER TABLE carts DROP COLUMN version;
