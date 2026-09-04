# Operator Authentication (P0.3)

This document is the demo credentials plus the design trade-offs for the
merchant operator auth added to close the "no authentication anywhere,
including on the money-gating endpoints" gap this project's audit pass
found: the approve/reject handlers used to read a client-supplied
`approver`/`by` string from the request body and default it to the
literal string `"operator"` if absent.

## Demo credentials

```
email:    owner@commerceos.demo
password: CommerceOS!2026
```

Seeded by `db/seeds/002_operator.sql` for `merchant_001` (the same
merchant every other seed file already targets). Run it after the
migration:

```
goose -dir db/migrations postgres "$DATABASE_URL" up
psql "$DATABASE_URL" -f db/seeds/001_catalog.sql   # if not already applied
psql "$DATABASE_URL" -f db/seeds/002_operator.sql
```

Sign in at `/dashboard` (any of the ten dashboard tabs redirect through
the same login wall — `frontend/app/dashboard/auth-gate.tsx`).

## What is and isn't gated

**Gated behind a logged-in operator** (`auth.Service.RequireOperator`):
`/dashboard/overview`, `/dashboard/metrics`, `/dashboard/experiment`, all
five `/safety/*` routes, `/audit/verify`, and the *list* endpoints —
`GET /approval-requests` and `GET /runs` (both expose every buyer's data
across the whole merchant, so they're operator-only by nature).

**Not gated — buyer checkout stays guest, on purpose:** the catalog,
cart, checkout, order-creation, payment, and order-history endpoints
(`/products`, `/carts/*`, `/orders`, `/orders/*/payment`, …), plus
`GET /approval-requests/{id}` and `GET /runs/{id}` — a buyer polling
their *own* approval request or looking at *their own* checkout's audit
trail needs neither a password nor an account. Buyer accounts/login were
explicitly scoped out of this pass (see the `AskUserQuestion` decision in
the session that built this: "Operator/merchant auth only").

**The interesting middle case — `POST /approval-requests/{id}/approve`
and `/reject`:** these two endpoints have *two* legitimate callers that
both existed before this change and both still need to work:

1. The buyer's own browser, self-confirming a Level 2/3 purchase
   (`checkout.tsx`'s `approveAndPay`/`rejectApproval`/`approveGateAndPay`).
2. A merchant operator reviewing from `/dashboard/approvals`.

Neither is "the operator" exclusively, so these routes are **not** behind
`RequireOperator`. They run through `auth.Service.OptionalOperator`
instead — it attaches a verified operator to the request context if a
valid bearer token is present, but never blocks an anonymous request —
and `backend/policy/service.go`'s `resolveApprover` decides, server-side,
whether the request is legitimate:

```go
func resolveApprover(req ApprovalRequest, cartID, operatorEmail string) (string, error) {
	if operatorEmail != "" {
		return "operator:" + operatorEmail, nil
	}
	if cartID != "" && cartID == req.CartID {
		return "buyer (cart " + cartID + " verified)", nil
	}
	return "", ErrApprovalUnauthorized
}
```

`operatorEmail` only ever has a value if `OptionalOperator` already
validated a real bearer session — the handler never trusts a client-sent
identity string for it. `cartID` is proof-by-construction: only the
browser that ran *that* cart through `/policy/mandates` and
`/policy/propose` knows its `cart_id`, so returning it back is equivalent
to "I am the buyer who created this". This replaced the previous
behavior, where both callers sent a free-text `approver`/`by` field the
client could set to literally any string, with zero verification.

## Why PBKDF2 instead of bcrypt

The obvious choice for password hashing is `golang.org/x/crypto/bcrypt`.
It isn't in `go.mod`, and this development environment has no working Go
toolchain and no network access to run `go get` — so a new dependency
can't be resolved into `go.sum` here, only asserted, which would produce
a `go.mod`/`go.sum` mismatch that fails `go build` for whoever pulls this
branch. Rather than ship an import that's never actually been verified to
build, `backend/auth/password.go` implements PBKDF2-HMAC-SHA256 (RFC
8018) by hand against the Go standard library only (`crypto/hmac`,
`crypto/sha256`, `crypto/rand`, `crypto/subtle`) — no third-party crypto
code, nothing hand-rolled at the primitive level, just the well-known KDF
composition. 210,000 iterations is OWASP's 2023-recommended minimum for
PBKDF2-HMAC-SHA256. This is a deliberate, documented trade-off for this
environment's constraints, not a security downgrade in principle — if
`golang.org/x/crypto` is added to `go.mod` later (trivial once a normal
Go+network environment is available), swapping `HashPassword`/
`VerifyPassword` to bcrypt or argon2id is a two-function change; nothing
else in `backend/auth/` depends on the specific algorithm.

## Multi-operator invites (item 40)

Until item 40, exactly one hardcoded operator existed per merchant
(`db/seeds/002_operator.sql`). A logged-in operator can now invite a
teammate to get their own account and login on the same merchant --
`backend/auth/invite.go`, migration
`db/migrations/20260902100000_create_operator_invites.sql`.

```
POST   /auth/invites         RequireOperator   create an invite (body: {"email": "..."})
GET    /auth/invites         RequireOperator   list this merchant's invites (pending/accepted/expired)
DELETE /auth/invites/{id}    RequireOperator   revoke a still-pending invite
POST   /auth/invites/accept  public            redeem a token + set a password -> new operator + session
GET    /auth/operators       RequireOperator   list this merchant's operators
DELETE /auth/operators/{id}  RequireOperator   remove a teammate
```

**No outbound email.** This project has no mail integration, and adding
one was treated as out of scope for this item (it would be its own
separate, judgeable piece of work). `POST /auth/invites` hands the raw
invite token back to the *inviting* operator once, in the response body
-- exactly like a session token, only its SHA-256 hash is ever
persisted (`operator_invites.token_hash`). The dashboard's Settings ->
Team panel (`frontend/app/dashboard/settings/team.tsx`) shows it as a
copyable `https://.../accept-invite?token=...` link for the operator to
send the teammate directly. `frontend/app/accept-invite/page.tsx` is the
public page that redeems it: the invitee sets a password (minimum 12
characters -- the one place in this package a brand-new credential is
actually chosen, unlike `Login`, which only ever verifies an
already-hashed one) and, on success, is signed in immediately, exactly
like a normal login.

**Invite lifetime and reuse.** An invite is valid for 7 days
(`auth.InviteTTL`) and can be redeemed exactly once -- `AcceptInvite`
checks both `expires_at` and `accepted_at` before creating the account,
and stamps `accepted_at` once it does. Revoking a pending invite
(`DELETE /auth/invites/{id}`) and creating a new one for the same email
is how a merchant reissues a stale link.

**Guardrails against a merchant losing dashboard access.**
`RemoveOperator` refuses to remove the caller's own account
(`ErrCannotRemoveSelf`) and refuses to remove a merchant's last
remaining operator at all (`ErrCannotRemoveLastOperator`) -- neither
guard existed before this item because there was never more than one
operator to remove. Every invite/operator endpoint is scoped to the
caller's own `merchant_id` (never client-supplied, always the operator
attached to the request context by `RequireOperator`), so one merchant's
operator can never invite into, list, or remove from a different
merchant's team by guessing an ID.

## Session model

Login (`POST /auth/login`) returns a 32-byte random token, hex-encoded,
valid for 24 hours (`auth.SessionTTL`). Only the SHA-256 hash of the
token is stored (`operator_sessions.token_hash`) — the raw token exists
only in the login response and the `Authorization: Bearer <token>` header
the browser sends back, mirroring how the password itself is never
stored raw. `POST /auth/logout` deletes the session row; logging out
twice, or with an already-invalid token, is not an error.

The frontend keeps the token in `localStorage`
(`frontend/lib/auth.ts`) — a static token in a single-tenant demo, not a
production session-management design (no CSRF concerns since it's a
bearer token in an `Authorization` header, not a cookie, but there's no
refresh flow, no revoke-all-sessions UI, and no rate limiting on
`/auth/login` beyond what a determined attacker would need to notice).
Good enough for a buildathon operator login; call out explicitly as a
known simplification if asked.
