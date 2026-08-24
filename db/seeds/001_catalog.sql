INSERT INTO merchants (id)
VALUES ('merchant_001')
ON CONFLICT (id) DO NOTHING;

-- Monetary amounts are paise. This unbound demo mandate makes the policy
-- endpoint usable immediately; checkout creates a narrower cart-bound mandate.
INSERT INTO mandates (
    id, buyer, merchant, allowed_categories, maximum_amount, currency,
    requires_confirmation_above, allowed_payment_methods, expires_at, purpose, status
)
VALUES (
    'mandate_demo', 'demo_buyer', 'merchant_001', '["electronics"]', 3000000, 'INR',
    3000000, '["card", "upi"]', '2030-01-01T00:00:00Z', 'Demo checkout mandate', 'ACTIVE'
)
ON CONFLICT (id) DO NOTHING;

INSERT INTO products (
    id,
    merchant_id,
    title,
    price_amount,
    price_currency,
    availability,
    features,
    compatibility,
    use_cases,
    return_policy,
    shipping,
    attributes,
    purchase_constraints
)
VALUES (
    'airpods-pro-2',
    'merchant_001',
    'AirPods Pro',
    2490000,
    'INR',
    12,
    '["active_noise_cancellation", "transparency_mode"]',
    '["ios", "macos"]',
    '["travel", "music", "calls"]',
    '{"days": 7}',
    '{"estimated_days": 3}',
    '{"anc": true, "battery_hours": 30, "wireless": true}',
    '{"max_quantity": 2}'
)
ON CONFLICT (id) DO NOTHING;

INSERT INTO product_variants (
    id,
    product_id,
    sku,
    price_amount,
    availability,
    attributes
)
VALUES (
    'airpods-pro-2-default',
    'airpods-pro-2',
    'AIRPODS-PRO-2',
    2490000,
    12,
    '{"color": "white"}'
)
ON CONFLICT (id) DO NOTHING;

-- AirPods Case
INSERT INTO products (
    id, merchant_id, title, price_amount, price_currency, availability,
    features, compatibility, use_cases, return_policy, shipping,
    attributes, purchase_constraints
)
VALUES (
    'airpods-case',
    'merchant_001',
    'AirPods Case',
    199900,
    'INR',
    25,
    '["protective", "wireless_charging"]',
    '["airpods_pro_2"]',
    '["protection", "travel"]',
    '{"days": 7}',
    '{"estimated_days": 3}',
    '{"material": "silicone", "wireless_charging": true}',
    '{"max_quantity": 3}'
)
ON CONFLICT (id) DO NOTHING;

INSERT INTO product_variants (
    id, product_id, sku, price_amount, availability, attributes
)
VALUES (
    'airpods-case-default',
    'airpods-case',
    'AIRPODS-CASE',
    199900,
    25,
    '{"color": "black"}'
)
ON CONFLICT (id) DO NOTHING;


-- AppleCare
INSERT INTO products (
    id, merchant_id, title, price_amount, price_currency, availability,
    features, compatibility, use_cases, return_policy, shipping,
    attributes, purchase_constraints
)
VALUES (
    'applecare',
    'merchant_001',
    'AppleCare',
    250000,
    'INR',
    50,
    '["extended_warranty", "technical_support"]',
    '["apple_devices"]',
    '["protection", "support"]',
    '{"days": 0}',
    '{"estimated_days": 0}',
    '{"coverage_years": 2}',
    '{"max_quantity": 1}'
)
ON CONFLICT (id) DO NOTHING;

INSERT INTO product_variants (
    id, product_id, sku, price_amount, availability, attributes
)
VALUES (
    'applecare-default',
    'applecare',
    'APPLECARE',
    250000,
    50,
    '{}'
)
ON CONFLICT (id) DO NOTHING;


-- USB-C Adapter
INSERT INTO products (
    id, merchant_id, title, price_amount, price_currency, availability,
    features, compatibility, use_cases, return_policy, shipping,
    attributes, purchase_constraints
)
VALUES (
    'usb-c-adapter',
    'merchant_001',
    'USB-C Adapter',
    129900,
    'INR',
    30,
    '["usb_c", "plug_and_play"]',
    '["macos", "windows", "usb_c_devices"]',
    '["charging", "connectivity", "travel"]',
    '{"days": 7}',
    '{"estimated_days": 3}',
    '{"ports": 1, "connector": "usb_c"}',
    '{"max_quantity": 4}'
)
ON CONFLICT (id) DO NOTHING;

INSERT INTO product_variants (
    id, product_id, sku, price_amount, availability, attributes
)
VALUES (
    'usb-c-adapter-default',
    'usb-c-adapter',
    'USB-C-ADAPTER',
    129900,
    30,
    '{"connector": "usb_c"}'
)
ON CONFLICT (id) DO NOTHING;
