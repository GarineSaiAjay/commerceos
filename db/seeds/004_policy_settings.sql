-- Seeds policy_settings with the exact values policy.DefaultConfig()
-- (backend/policy/model.go) already hardcodes, so behavior is
-- byte-for-byte unchanged for a fresh database until an operator
-- actually changes something from /dashboard/settings -- see item 25
-- (ROADMAP-PRIORITIZED.md P2, PLAN-05-SELLER-DASHBOARD.md §4).
--
-- Self-contained, same convention as 002_operator.sql: don't depend on
-- 001_catalog.sql having run first for the merchants FK.
INSERT INTO merchants (id)
VALUES ('merchant_001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO policy_settings (
    merchant_id, ceiling, budget_tolerance, allowed_currencies,
    allowed_merchants, updated_by
)
VALUES (
    'merchant_001',
    3000000,
    0,
    '["INR"]',
    '["merchant_001"]',
    'seed'
)
ON CONFLICT (merchant_id) DO NOTHING;
