import CheckoutFlow from "./checkout";

export const dynamic = "force-dynamic";

interface Product {
  product_id: string;
  title: string;
  price: { amount: number; currency: string };
  availability: number;
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

export default async function Home() {
  let products: Product[] = [];

  try {
    const res = await fetch(`${API_BASE}/products`, { cache: "no-store" });
    if (res.ok) {
      products = await res.json();
    }
  } catch {
    // Catalog fetch failed; the client flow will surface an empty state.
  }

  return <CheckoutFlow initialProducts={products} />;
}