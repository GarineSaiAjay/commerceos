package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// ProductsCache caches ListProducts' full result for a short TTL
// (item 23, PLAN-04-UI-UX-AND-LATENCY.md §B2). ListProducts today is a
// real N+1 -- one query for every product ID, then one GetProduct call
// per row, each of which itself runs a JOIN plus a second query for
// that product's variants (see PostgresRepository.ListProducts/
// GetProduct) -- so caching its result meaningfully cuts DB load under
// concurrent traffic, not just perceived latency.
//
// A narrow interface, not a concrete *RedisProductsCache field on
// Service, so a unit test can fake it without a real Redis instance
// (the same reasoning agents.RunRecorder and policy.Repository's
// interfaces already follow in this codebase).
type ProductsCache interface {
	// Get returns the cached product list and true on a cache hit, or
	// (nil, false) on a miss -- including when the cache itself is
	// unreachable, which must degrade to "always fetch from the
	// repository" exactly like a miss, never fail the request the way
	// a required dependency would.
	Get(ctx context.Context) ([]Product, bool)

	// Set stores products for the cache's configured TTL. Best-effort:
	// a failed Set just means the next Get misses and ListProducts
	// falls through to the repository again, same as if the cache
	// were never configured at all.
	Set(ctx context.Context, products []Product)

	// Invalidate clears the cached list. Called after every successful
	// CreateProduct/UpdateProduct/DeleteProduct so a merchant's own
	// edit (e.g. frontend/app/dashboard/catalog/page.tsx, item 14) is
	// never masked by a stale cached list for up to the TTL -- the
	// same correctness requirement the plan doc calls out explicitly.
	Invalidate(ctx context.Context)
}

// productsCacheKey is a fixed key -- this catalog is single-merchant
// today (MERCHANT_ID = "merchant_001" is hardcoded across this whole
// project, see files/AUTH.md), so there is exactly one "the product
// list" to cache, not one per merchant. A real multi-merchant catalog
// would need this keyed by merchant ID.
const productsCacheKey = "catalog:products:v1"

// RedisProductsCache backs ProductsCache with the same Redis instance
// already provisioned for the Streams event bus
// (infra/docker-compose.yml) but, until this item, never used for
// anything else.
//
// Caching AverageRating/ReviewCount (computed at read time from a live
// join against the reviews table, see Product's doc comment) alongside
// the rest of a Product is a deliberate, honest trade: a review
// submitted mid-TTL won't move a cached product's rating until the
// cache expires or the next unrelated catalog mutation invalidates it.
// reviews aren't written through catalog.Service at all, so there is
// no natural invalidation hook for them here -- this is the same
// acceptable few-seconds staleness window any storefront catalog
// cache carries, which is exactly why the TTL is short (5-10s), not
// long.
type RedisProductsCache struct {
	client *redis.Client
	ttl    time.Duration
}

// NewRedisProductsCache builds a cache with the given TTL. Wire this
// in with Service.WithCache; a Service that never calls WithCache
// behaves identically to before this item.
func NewRedisProductsCache(client *redis.Client, ttl time.Duration) *RedisProductsCache {
	return &RedisProductsCache{client: client, ttl: ttl}
}

func (c *RedisProductsCache) Get(ctx context.Context) ([]Product, bool) {
	raw, err := c.client.Get(ctx, productsCacheKey).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			log.Printf("[catalog] products cache read failed, falling back to the repository: %v", err)
		}
		return nil, false
	}

	var products []Product
	if err := json.Unmarshal(raw, &products); err != nil {
		log.Printf("[catalog] products cache held undecodable data, falling back to the repository: %v", err)
		return nil, false
	}

	return products, true
}

func (c *RedisProductsCache) Set(ctx context.Context, products []Product) {
	raw, err := json.Marshal(products)
	if err != nil {
		log.Printf("[catalog] failed to marshal products for caching: %v", err)
		return
	}
	if err := c.client.Set(ctx, productsCacheKey, raw, c.ttl).Err(); err != nil {
		log.Printf("[catalog] products cache write failed (non-fatal, next read hits the repository): %v", err)
	}
}

func (c *RedisProductsCache) Invalidate(ctx context.Context) {
	if err := c.client.Del(ctx, productsCacheKey).Err(); err != nil {
		log.Printf("[catalog] products cache invalidation failed -- a stale list may be served for up to the TTL: %v", err)
	}
}
