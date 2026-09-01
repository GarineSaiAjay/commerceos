#!/usr/bin/env python3
"""Regenerate policy.DefaultConfig()'s AllowedProducts list from the seed
catalog, so it can never silently go stale again the way it has three
times now (see checkProducts's doc comment in backend/policy/engine.go
and PolicyConfig.AllowedProducts's in backend/policy/model.go).

This is a generator, not a runtime dependency (PLAN-02-CATALOG-AND-
COMMERCE.md section 6): Engine itself still never reads the seed file or
touches a database to build this list -- it stays exactly as
dependency-free and deterministic as it always has. This script only
ever runs at commit time (by hand) or in CI (as a diff-check, see
.github/workflows/ci.yml's "Check catalog/allowlist sync" step): it
reads db/seeds/001_catalog.sql's product IDs and rewrites the
AllowedProducts literal in backend/policy/model.go to match, in the
same order the products appear in the seed file. If a developer adds a
product to the seed catalog and forgets to run this, CI's git diff
--exit-code on model.go after running it fails the build instead of
the mismatch silently shipping as a production bug (which is exactly
what happened after airpods-pro-3/airtag-4pack/beats-fit-pro were
added -- see engine.go's checkProducts doc comment for that history).

Usage:
    python3 scripts/gen_catalog_allowlist.py [--check]

Without --check: rewrites backend/policy/model.go in place (no-op if
already in sync) and prints what it did.

With --check: does not write anything; exits 1 and prints a diff-style
message if the committed file is out of sync, exits 0 if it already
matches. Kept for local convenience -- CI itself uses the simpler
"regenerate, then git diff --exit-code" pattern (see the workflow file)
since that is also the exact recipe a developer runs locally to fix a
failure, not just detect one.
"""
import argparse
import re
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
SEED_PATH = REPO_ROOT / "db" / "seeds" / "001_catalog.sql"
MODEL_PATH = REPO_ROOT / "backend" / "policy" / "model.go"

# Matches "INSERT INTO products (" ... ") VALUES ( 'product-id', ..." --
# deliberately anchored on "products (" (with the space) so it can never
# match "INSERT INTO product_variants (" (no space before the paren in
# that table name), which is the other INSERT this file contains and
# must NOT contribute an entry: variants aren't products, and several
# products below have colorway/tier variants inserted separately, later
# in the file, that would otherwise duplicate the parent product's ID.
PRODUCT_INSERT_RE = re.compile(
    r"INSERT INTO products\s*\([^)]*\)\s*VALUES\s*\(\s*'([^']+)'",
    re.DOTALL,
)

ALLOWED_PRODUCTS_RE = re.compile(
    r"(\t\tAllowedProducts: \[\]string\{\n)(.*?\n)(\t\t\},\n)",
    re.DOTALL,
)


def product_ids_from_seed(seed_sql: str) -> list[str]:
    ids = PRODUCT_INSERT_RE.findall(seed_sql)
    if not ids:
        raise SystemExit(f"found zero product IDs in {SEED_PATH} -- regex is broken, refusing to write an empty allowlist")
    seen = set()
    deduped = []
    for pid in ids:
        if pid in seen:
            raise SystemExit(f"product id {pid!r} INSERT'd twice in {SEED_PATH} -- fix the seed file")
        seen.add(pid)
        deduped.append(pid)
    return deduped


def render_block(ids: list[str]) -> str:
    return "".join(f'\t\t\t"{pid}",\n' for pid in ids)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true", help="report sync status without writing")
    args = parser.parse_args()

    seed_sql = SEED_PATH.read_text(encoding="utf-8")
    model_go = MODEL_PATH.read_text(encoding="utf-8")

    ids = product_ids_from_seed(seed_sql)
    new_block = render_block(ids)

    match = ALLOWED_PRODUCTS_RE.search(model_go)
    if not match:
        raise SystemExit(
            f"could not find an `AllowedProducts: []string{{ ... }},` block in {MODEL_PATH} "
            "-- has DefaultConfig()'s formatting changed? update ALLOWED_PRODUCTS_RE in this script to match."
        )

    current_block = match.group(2)
    if args.check:
        if current_block == new_block:
            print(f"{MODEL_PATH}: AllowedProducts is in sync with {SEED_PATH} ({len(ids)} products)")
            return 0
        print(f"{MODEL_PATH}: AllowedProducts is OUT OF SYNC with {SEED_PATH}")
        print(f"  seed catalog has {len(ids)} products: {ids}")
        print("  run: python3 scripts/gen_catalog_allowlist.py")
        return 1

    new_model_go = model_go[: match.start()] + match.group(1) + new_block + match.group(3) + model_go[match.end() :]
    if new_model_go == model_go:
        print(f"{MODEL_PATH}: already in sync with {SEED_PATH} ({len(ids)} products), nothing to do")
        return 0

    MODEL_PATH.write_text(new_model_go, encoding="utf-8")
    print(f"{MODEL_PATH}: rewrote AllowedProducts to match {SEED_PATH} ({len(ids)} products)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
