import CheckoutFlow from "./checkout";

export const dynamic = "force-dynamic";

interface Product {
  product_id: string;
  title: string;
  price: { amount: number; currency: string };
  availability: number;
  average_rating?: number;
  review_count?: number;
}

// This fetch runs server-side (inside the Next.js server process), not
// in the browser -- when the frontend runs in its own container
// (infra/docker-compose.yml), "localhost" here means this container,
// not the backend one, so the previous hardcoded
// "http://localhost:8081" silently failed (caught below) and always
// rendered an empty catalog. COMMERCE_SERVICE_URL is a server-only env
// var (deliberately not NEXT_PUBLIC_-prefixed, so it's never inlined
// into the client bundle) that docker-compose.yml's frontend service
// sets to "http://backend:8081" -- Compose's internal DNS name for the
// backend container. Outside Docker (frontend run directly on the
// host via `npm run dev`), it's unset and the localhost:8081 default
// is correct again, since both processes share the host's network.
const API_BASE = process.env.COMMERCE_SERVICE_URL ?? "http://localhost:8081";

// Item 23 (PLAN-04-UI-UX-AND-LATENCY.md §B2, "client-side" layer): a
// short in-memory TTL cache of this exact fetch. This runs inside the
// Next.js server process (see the comment above), not a buyer's
// browser -- checkout.tsx itself never re-fetches the catalog
// client-side (`const [products] = useState(initialProducts)`, no
// client fetch at all), so a browser-side cache would have nothing to
// cache against. A module-level variable here instead caches across
// the repeated requests this Next.js server process handles: multiple
// page loads/refreshes within the TTL reuse the same result instead
// of each one hitting the backend. This is genuinely a second,
// independent layer from catalog/service.go's new Redis cache -- even
// a miss here now often still avoids a real Postgres round trip.
let cachedProducts: { data: Product[]; expiresAt: number } | null = null;
const PRODUCTS_CACHE_TTL_MS = 30_000;

async function fetchProducts(): Promise<Product[]> {
  if (cachedProducts && cachedProducts.expiresAt > Date.now()) {
    return cachedProducts.data;
  }

  let products: Product[] = [];
  try {
    const res = await fetch(`${API_BASE}/products`, { cache: "no-store" });
    if (res.ok) {
      products = await res.json();
    }
  } catch {
    // Catalog fetch failed; the client flow will surface an empty
    // state. Deliberately not cached -- the next request should retry
    // rather than serve an empty catalog for the rest of the TTL.
    return products;
  }

  cachedProducts = { data: products, expiresAt: Date.now() + PRODUCTS_CACHE_TTL_MS };
  return products;
}

export default async function Home() {
  const products = await fetchProducts();
  return <CheckoutFlow initialProducts={products} />;
}