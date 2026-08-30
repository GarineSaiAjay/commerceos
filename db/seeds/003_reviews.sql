-- A small operator-seeded starter set of reviews (PLAN-02-CATALOG-AND-
-- COMMERCE.md §2) so the catalog doesn't show an empty rating state on
-- first run. Deliberately order_id = NULL -- verified_purchase is
-- always `order_id IS NOT NULL` at read time, so these are honestly
-- distinguishable from a real buyer's post-checkout review; real
-- reviews accumulate on top of this set the longer a demo/judging
-- session runs (POST /orders/{id}/review).
--
-- reviews.id is a bare BIGSERIAL with no natural key to ON CONFLICT
-- on, unlike products/mandates/merchants above -- idempotency here
-- instead comes from a per-row NOT EXISTS guard keyed on
-- (product_id, order_id IS NULL, buyer_reference), which is exactly as
-- safe to re-run as the ON CONFLICT DO NOTHING pattern used elsewhere
-- in this directory.

INSERT INTO reviews (product_id, order_id, buyer_reference, rating, comment)
SELECT 'airpods-pro-2', NULL, 'Priya S.', 5, 'ANC is fantastic for my daily commute, and the case fits my pocket fine.'
WHERE NOT EXISTS (
    SELECT 1 FROM reviews WHERE product_id = 'airpods-pro-2' AND order_id IS NULL AND buyer_reference = 'Priya S.'
);

INSERT INTO reviews (product_id, order_id, buyer_reference, rating, comment)
SELECT 'airpods-pro-2', NULL, 'Rohit K.', 4, 'Great sound, battery life could be a bit better with ANC always on.'
WHERE NOT EXISTS (
    SELECT 1 FROM reviews WHERE product_id = 'airpods-pro-2' AND order_id IS NULL AND buyer_reference = 'Rohit K.'
);

INSERT INTO reviews (product_id, order_id, buyer_reference, rating, comment)
SELECT 'beats-fit-pro', NULL, 'Ananya M.', 5, 'Secure fit stayed put through an entire HIIT class -- exactly what I needed.'
WHERE NOT EXISTS (
    SELECT 1 FROM reviews WHERE product_id = 'beats-fit-pro' AND order_id IS NULL AND buyer_reference = 'Ananya M.'
);

INSERT INTO reviews (product_id, order_id, buyer_reference, rating, comment)
SELECT 'magsafe-charger', NULL, 'Devika R.', 5, 'Snaps into alignment instantly, charges faster than my old pad.'
WHERE NOT EXISTS (
    SELECT 1 FROM reviews WHERE product_id = 'magsafe-charger' AND order_id IS NULL AND buyer_reference = 'Devika R.'
);

INSERT INTO reviews (product_id, order_id, buyer_reference, rating, comment)
SELECT 'airtag-4pack', NULL, 'Karan V.', 5, 'Put one in every bag -- found my backpack in a taxi within minutes.'
WHERE NOT EXISTS (
    SELECT 1 FROM reviews WHERE product_id = 'airtag-4pack' AND order_id IS NULL AND buyer_reference = 'Karan V.'
);

INSERT INTO reviews (product_id, order_id, buyer_reference, rating, comment)
SELECT 'airtag-4pack', NULL, 'Meera N.', 3, 'Works as advertised, though the battery cover is fiddly to remove.'
WHERE NOT EXISTS (
    SELECT 1 FROM reviews WHERE product_id = 'airtag-4pack' AND order_id IS NULL AND buyer_reference = 'Meera N.'
);

INSERT INTO reviews (product_id, order_id, buyer_reference, rating, comment)
SELECT 'applecare', NULL, 'Sanjay T.', 4, 'Peace of mind for the price -- claims process was straightforward when I needed it.'
WHERE NOT EXISTS (
    SELECT 1 FROM reviews WHERE product_id = 'applecare' AND order_id IS NULL AND buyer_reference = 'Sanjay T.'
);

INSERT INTO reviews (product_id, order_id, buyer_reference, rating, comment)
SELECT 'wireless-charging-pad', NULL, 'Nikhil P.', 4, 'Charges through my case without issue, LED indicator is a nice touch.'
WHERE NOT EXISTS (
    SELECT 1 FROM reviews WHERE product_id = 'wireless-charging-pad' AND order_id IS NULL AND buyer_reference = 'Nikhil P.'
);
