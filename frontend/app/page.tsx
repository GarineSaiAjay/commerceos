import CheckoutFlow from "./checkout";

export const dynamic = "force-dynamic";

interface Product {
  product_id: string;
  title: string;
  price: { amount: number; currency: string };
  availability: number;
}

const API_BASE = "http://localhost:8081";

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