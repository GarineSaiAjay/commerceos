-- Demo operator account for merchant_001's dashboard/approvals login.
-- Password hashing: PBKDF2-HMAC-SHA256, 210,000 iterations (OWASP's 2023
-- minimum), implemented against the Go standard library only in
-- backend/auth/password.go -- this environment cannot fetch new Go
-- modules (no network access to golang.org/x/crypto/bcrypt), so this is
-- a deliberate, documented trade-off rather than a weaker home-rolled
-- scheme. See files/AUTH.md.
--
-- Demo credentials (files/AUTH.md):
--   email:    owner@commerceos.demo
--   password: CommerceOS!2026
-- Self-contained: don't depend on 001_catalog.sql having run first.
-- Without this, applying this seed before 001_catalog.sql fails the
-- INSERT below on the merchant_id foreign key -- silently, from the
-- operator's point of view: the table stays empty and every login
-- attempt afterward gets the generic "invalid email or password" with
-- no indication that no operator row exists at all.
INSERT INTO merchants (id)
VALUES ('merchant_001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO operators (id, merchant_id, email, password_hash)
VALUES (
    'operator_merchant_001_owner',
    'merchant_001',
    'owner@commerceos.demo',
    'pbkdf2-sha256$210000$6b9f1920c06e596decf34b64d9a17d1e$bf929578cfd15291af2fc21e9cfc34b093cc214cae5954c97f58ac47832c6d37'
)
ON CONFLICT (id) DO NOTHING;
