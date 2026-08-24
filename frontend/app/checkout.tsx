"use client";

import { useState } from "react";

declare global {
  interface Window {
    Razorpay: new (options: RazorpayOptions) => RazorpayInstance;
  }
}

interface RazorpayOptions {
  key: string;
  amount: number;
  currency: string;
  name: string;
  description: string;
  order_id: string;
  handler: (response: RazorpayResponse) => void;
  modal?: {
    ondismiss?: () => void;
  };
  prefill?: {
    name?: string;
    email?: string;
    contact?: string;
  };
  theme?: {
    color?: string;
  };
}

interface RazorpayResponse {
  razorpay_payment_id: string;
  razorpay_order_id: string;
  razorpay_signature: string;
}

interface RazorpayInstance {
  open: () => void;
}

interface Product {
  product_id: string;
  title: string;
  price: { amount: number; currency: string };
  availability: number;
}

interface CartItem {
  product_id: string;
  variant_id: string;
  title: string;
  quantity: number;
  unit_price: number;
  total: number;
}

interface Cart {
  cart_id: string;
  items: CartItem[];
  subtotal: number;
  currency: string;
}

interface Order {
  order_id: string;
  cart_id: string;
  status: string;
  subtotal: number;
  currency: string;
  items: CartItem[];
}

interface Payment {
  payment_id: string;
  order_id: string;
  provider_order_id: string;
  amount: number;
  currency: string;
  status: string;
  key_id: string;
}

interface Recovery {
  order_id: string;
  payment_status: string;
  attempt_status: string;
  error_code: string;
  error_description: string;
  safe_message: string;
  reservation_expires_at: string;
  retry_allowed: boolean;
  cart: { subtotal: number; currency: string; items: { product_id: string; variant_id: string; title: string; quantity: number; unit_price: number; total: number }[] };
}

interface Mandate {
  mandate_id: string;
}

interface Decision {
  decision: string;
  authorization_id: string;
  approval_request_id: string;
  reason: string;
}

const API_BASE = "http://localhost:8081";
const MERCHANT_ID = "merchant_001";

// Each order uses a fresh cart ID. A cart is single-use: once it is
// checked out it is marked `checked_out` and can never be reused, so a
// fixed ID would leave the UI stuck on a stale, already-checked-out cart.
function freshCartId() {
  return `cart_${Date.now()}`;
}

type Step = "catalog" | "cart" | "checkout" | "approval" | "pay" | "complete" | "failed";

export default function CheckoutFlow({
  initialProducts,
}: {
  initialProducts: Product[];
}) {
  const [step, setStep] = useState<Step>("catalog");
  const [products] = useState<Product[]>(initialProducts);
  const [cartId, setCartId] = useState<string>(() => freshCartId());
  const [cart, setCart] = useState<Cart | null>(null);
  const [order, setOrder] = useState<Order | null>(null);
  const [payment, setPayment] = useState<Payment | null>(null);
  	const [approvalRequestId, setApprovalRequestId] = useState("");
  	const [approvalReason, setApprovalReason] = useState("");
  	const [recovery, setRecovery] = useState<Recovery | null>(null);
  	const [loading, setLoading] = useState(false);
  	const [message, setMessage] = useState("");

  async function ensureCart() {
    try {
      const res = await fetch(`${API_BASE}/carts/${cartId}`);
      if (res.status === 404) {
        const createRes = await fetch(`${API_BASE}/carts`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            cart_id: cartId,
            merchant_id: MERCHANT_ID,
            currency: "INR",
          }),
        });
        if (!createRes.ok) throw new Error("Failed to create cart");
        return (await createRes.json()) as Cart;
      }
      if (!res.ok) throw new Error("Failed to load cart");
      return (await res.json()) as Cart;
    } catch (error) {
      throw error instanceof Error ? error : new Error("Failed to load cart");
    }
  }

  async function addToCart(product: Product) {
    setLoading(true);
    setMessage("");
    try {
      await ensureCart();
      const variantId = `${product.product_id}-default`;

      const res = await fetch(`${API_BASE}/carts/${cartId}/items`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          product_id: product.product_id,
          variant_id: variantId,
          title: product.title,
          quantity: 1,
        }),
      });
      if (!res.ok) throw new Error("Failed to add item to cart");

      setCart(await fetch(`${API_BASE}/carts/${cartId}`).then((r) => r.json()));
      setStep("cart");
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Failed to add item");
    } finally {
      setLoading(false);
    }
  }

  async function proceedToCheckout() {
    setLoading(true);
    setMessage("");
    try {
      const res = await fetch(`${API_BASE}/carts/${cartId}/checkout`, {
        method: "POST",
      });
      if (!res.ok) throw new Error("Failed to create order");
      const ord = (await res.json()) as Order;
      setOrder(ord);
      setStep("checkout");
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Checkout failed");
    } finally {
      setLoading(false);
    }
  }

  async function startPayment() {
    setLoading(true);
    setMessage("Creating payment...");
    try {
      const mandateRes = await fetch(`${API_BASE}/policy/mandates`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          buyer: "checkout_user",
          merchant: MERCHANT_ID,
          maximum_amount: order!.subtotal,
          currency: order!.currency,
          allowed_payment_methods: ["card", "upi"],
          purpose: `Checkout ${order!.order_id}`,
          cart_id: order!.cart_id,
        }),
      });
      if (!mandateRes.ok) throw new Error(await mandateRes.text());
      const mandate = (await mandateRes.json()) as Mandate;

      		const authorizationRes = await fetch(`${API_BASE}/policy/propose`, {
      			method: "POST",
      			headers: { "Content-Type": "application/json" },
      			body: JSON.stringify({
      				action: "CREATE_ORDER",
      				amount: order!.subtotal,
      				currency: order!.currency,
      				merchant: MERCHANT_ID,
      				items: order!.items.map((item) => item.product_id),
      				mandate_id: mandate.mandate_id,
      				cart_id: order!.cart_id,
      			}),
      		});
      		if (!authorizationRes.ok) throw new Error(await authorizationRes.text());
      		const decision = (await authorizationRes.json()) as Decision;

      		// Level 2/3 → durable human approval required before an
      		// authorization is issued.
      		if (decision.decision === "PENDING_HUMAN_APPROVAL") {
      			if (!decision.approval_request_id) {
      				throw new Error("Approval required but no request was created");
      			}
      			setApprovalRequestId(decision.approval_request_id);
      			setApprovalReason(decision.reason || "This purchase requires operator approval.");
      			setStep("approval");
      			setLoading(false);
      			return;
      		}

      		if (decision.decision !== "APPROVED" || !decision.authorization_id) {
      			throw new Error(decision.reason || "Payment was not authorized");
      		}

		await createPaymentWithLaunch(decision.authorization_id);
	} catch (error) {
      					const msg = error instanceof Error ? error.message : "Payment failed";
      					setMessage(msg);
      					setLoading(false);
      				}
      			}

      			// Approve a Level 2/3 request, then continue to payment.
      			async function approveAndPay() {
      				setLoading(true);
      				setMessage("Approving payment...");
      				try {
      					const res = await fetch(`${API_BASE}/approval-requests/${approvalRequestId}/approve`, {
      						method: "POST",
      						headers: { "Content-Type": "application/json" },
      						body: JSON.stringify({ approver: "buyer" }),
      					});
      					if (!res.ok) throw new Error(await res.text());
      					const decision = (await res.json()) as Decision;
      					if (decision.decision !== "APPROVED" || !decision.authorization_id) {
      						throw new Error(decision.reason || "Approval did not produce an authorization");
      					}
      					await createPaymentWithLaunch(decision.authorization_id);
      				} catch (error) {
      					setMessage(error instanceof Error ? error.message : "Approval failed");
      					setLoading(false);
      				}
      			}

      			// Reject a Level 2/3 request, returning to the catalog.
      			async function rejectApproval() {
      				setLoading(true);
      				setMessage("");
      				try {
      					await fetch(`${API_BASE}/approval-requests/${approvalRequestId}/reject`, {
      						method: "POST",
      						headers: { "Content-Type": "application/json" },
      						body: JSON.stringify({ by: "buyer", reason: "cancelled at approval screen" }),
      					});
      				} catch {
      					// best-effort; the cart is abandoned either way
      				}
      				resetToCatalog("Purchase cancelled. The approval was not granted.");
      			}

      			// Create the Razorpay order and open the Standard Checkout UI.
      			async function createPaymentWithLaunch(authId: string) {
      				const res = await fetch(`${API_BASE}/orders/${order!.order_id}/payment`, {
      					method: "POST",
      					headers: {
      						"Authorization-Id": authId,
      						"Idempotency-Key": `payment_${order!.order_id}`,
      					},
      				});
      				if (!res.ok) throw new Error("Failed to create payment");
      				const pay = (await res.json()) as Payment;
      				setPayment(pay);
      				setStep("pay");
      				setLoading(false);

      				// Open Razorpay Standard Checkout with the server-created order.
      				const options: RazorpayOptions = {
      					key: pay.key_id,
      					amount: pay.amount,
      					currency: pay.currency,
      					name: "CommerceOS",
      					description: `Order ${pay.order_id}`,
      					order_id: pay.provider_order_id,
      					handler: async (response) => {
      						await verifyPayment(response);
      					},
					modal: {
						ondismiss: () => {
							setStep("failed");
							loadRecovery();
							setLoading(false);
						},
					},
      					theme: { color: "#000000" },
      				};

      				const razorpay = new window.Razorpay(options);
      				razorpay.open();
      			}

      			// Fetch the authoritative recovery view from the server.
			async function loadRecovery() {
				if (!order) return;
				try {
					const res = await fetch(`${API_BASE}/orders/${order.order_id}/recovery`, { cache: "no-store" });
					if (res.ok) {
						setRecovery((await res.json()) as Recovery);
					}
				} catch {
					// fall back to the static message; recovery stays null
				}
			}

			async function resetToCatalog(messageText: string) {
      				setStep("catalog");
      				setCartId(freshCartId());
      				setCart(null);
      				setOrder(null);
      				setPayment(null);
      				setApprovalRequestId("");
      				setMessage(messageText);
      			}

  async function verifyPayment(response: RazorpayResponse) {
    setMessage("Verifying payment...");
    try {
      const res = await fetch(
        `${API_BASE}/orders/${order!.order_id}/payment/verify`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            razorpay_payment_id: response.razorpay_payment_id,
            razorpay_order_id: response.razorpay_order_id,
            razorpay_signature: response.razorpay_signature,
          }),
        },
      );
      if (!res.ok) throw new Error("Payment verification failed");
      const verified = (await res.json()) as Payment;
      setPayment(verified);
      setStep("complete");
    } catch (error) {
      setMessage(error instanceof Error ? error.message : "Verification failed");
    }
  }

  function formatINR(amount: number) {
    return `₹${(amount / 100).toFixed(2)}`;
  }

  return (
    <main className="min-h-screen bg-zinc-100">
      <div className="mx-auto max-w-3xl px-6 py-10">
        <header className="mb-8">
          <p className="text-sm font-medium text-zinc-500">CommerceOS</p>
          <h1 className="mt-2 text-3xl font-bold tracking-tight text-zinc-900">
            {step === "complete" ? "Order Complete" : "Checkout"}
          </h1>
        </header>

        {message && (
          <div className="mb-6 rounded-lg bg-zinc-100 p-4 text-sm text-zinc-700">
            {message}
          </div>
        )}

        {step === "catalog" && (
          <section>
            <h2 className="mb-4 text-lg font-semibold text-zinc-900">
              Browse Catalog
            </h2>
            {products.length === 0 ? (
              <p className="text-sm text-zinc-500">
                No products available. Ensure the Commerce Service is running
                and the catalog is seeded.
              </p>
            ) : (
              <ul className="divide-y divide-zinc-200">
                {products.map((product) => (
                  <li
                    key={product.product_id}
                    className="flex items-center justify-between py-4"
                  >
                    <div>
                      <p className="font-semibold text-zinc-900">
                        {product.title}
                      </p>
                      <p className="text-sm text-zinc-500">
                        {formatINR(product.price.amount)} · {product.availability} in stock
                      </p>
                    </div>
                    <button
                      onClick={() => addToCart(product)}
                      disabled={loading}
                      className="rounded-lg bg-black px-4 py-2 text-sm font-medium text-white transition hover:bg-zinc-800 disabled:opacity-50"
                    >
                      Add to cart
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </section>
        )}

        {step === "cart" && cart && (
          <section>
            <h2 className="mb-4 text-lg font-semibold text-zinc-900">
              Your Cart
            </h2>
            <ul className="divide-y divide-zinc-200">
              {cart.items.map((item) => (
                <li
                  key={item.variant_id}
                  className="flex items-center justify-between py-4"
                >
                  <div>
                    <p className="font-semibold text-zinc-900">{item.title}</p>
                    <p className="text-sm text-zinc-500">
                      Qty {item.quantity} × {formatINR(item.unit_price)}
                    </p>
                  </div>
                  <p className="font-semibold text-zinc-900">
                    {formatINR(item.total)}
                  </p>
                </li>
              ))}
            </ul>
            <div className="mt-4 flex items-center justify-between rounded-xl border border-zinc-200 p-5">
              <p className="font-semibold text-zinc-900">Subtotal</p>
              <p className="text-lg font-semibold text-zinc-900">
                {formatINR(cart.subtotal)}
              </p>
            </div>
            <div className="mt-6 flex gap-3">
              <button
                onClick={() => setStep("catalog")}
                className="rounded-lg border border-zinc-300 px-5 py-3 font-medium text-zinc-700 hover:bg-zinc-100"
              >
                Keep shopping
              </button>
              <button
                onClick={proceedToCheckout}
                disabled={loading || cart.items.length === 0}
                className="rounded-lg bg-black px-5 py-3 font-medium text-white transition hover:bg-zinc-800 disabled:opacity-50"
              >
                Checkout
              </button>
            </div>
          </section>
        )}

        {step === "checkout" && order && (
          <section>
            <h2 className="mb-4 text-lg font-semibold text-zinc-900">
              Confirm Order
            </h2>
            <div className="rounded-xl border border-zinc-200 p-5">
              <div className="flex items-center justify-between">
                <div>
                  <p className="font-semibold text-zinc-900">
                    Order {order.order_id}
                  </p>
                  <p className="mt-1 text-sm text-zinc-500">
                    Status: {order.status}
                  </p>
                </div>
                <p className="text-lg font-semibold text-zinc-900">
                  {formatINR(order.subtotal)}
                </p>
              </div>
            </div>
            <button
              onClick={startPayment}
              disabled={loading}
              className="mt-6 w-full rounded-xl bg-black px-5 py-3.5 font-medium text-white transition hover:bg-zinc-800 disabled:opacity-50"
            >
              {loading ? "Processing..." : `Pay ${formatINR(order.subtotal)}`}
            </button>
          </section>
        )}

        {step === "approval" && (
          <section>
            <h2 className="mb-4 text-lg font-semibold text-zinc-900">
              Purchase Requires Approval
            </h2>
            <div className="rounded-xl border border-amber-200 bg-amber-50 p-5">
              <p className="text-sm text-amber-900">
                This order is above the auto-approval threshold. An operator
                must approve it before payment can be initiated. {approvalReason}
              </p>
              {order && (
                <dl className="mt-4 grid gap-2 text-sm text-amber-900 sm:grid-cols-2">
                  <div><dt className="font-medium">Order</dt><dd>{order.order_id}</dd></div>
                  <div><dt className="font-medium">Total</dt><dd>{formatINR(order.subtotal)}</dd></div>
                  <div><dt className="font-medium">Approval request</dt><dd className="font-mono">{approvalRequestId}</dd></div>
                </dl>
              )}
            </div>
            <div className="mt-6 space-y-3">
              <button
                onClick={approveAndPay}
                disabled={loading}
                className="w-full rounded-xl bg-black px-5 py-3.5 font-medium text-white transition hover:bg-zinc-800 disabled:opacity-50"
              >
                {loading ? "Approving..." : `Approve & Pay ${order ? formatINR(order.subtotal) : ""}`}
              </button>
              <button
                onClick={rejectApproval}
                disabled={loading}
                className="w-full rounded-xl border border-zinc-300 px-5 py-3 font-medium text-zinc-700 hover:bg-zinc-100"
              >
                Reject
              </button>
            </div>
          </section>
        )}

        {step === "pay" && payment && (
          <section>
            <h2 className="mb-4 text-lg font-semibold text-zinc-900">
              Complete Payment
            </h2>
            <p className="text-sm text-zinc-500">
              The Razorpay checkout window should have opened. Complete the
              payment there to finish your order.
            </p>
            <div className="mt-4 rounded-xl border border-zinc-200 p-5">
              <p className="text-sm text-zinc-500">Amount due</p>
              <p className="text-2xl font-bold text-zinc-900">
                {formatINR(payment.amount)}
              </p>
            </div>
            <button
              onClick={() => {
                setStep("catalog");
                setCartId(freshCartId());
                setCart(null);
                setOrder(null);
                setPayment(null);
                setMessage("Payment cancelled. Your cart was not charged.");
              }}
              className="mt-4 w-full rounded-xl border border-zinc-300 px-5 py-3 font-medium text-zinc-700 hover:bg-zinc-100"
            >
              Cancel payment
            </button>
          </section>
        )}

	{step === "failed" && (
		<section>
			<h2 className="mb-4 text-lg font-semibold text-zinc-900">
				Payment wasn&apos;t completed
			</h2>
			<div className="rounded-xl border border-amber-200 bg-amber-50 p-5">
				<p className="text-sm text-amber-900">
					{recovery ? recovery.safe_message : "Razorpay reported that the payment failed. Your order has not been charged twice. The cart remains reserved for 9 minutes."}
				</p>
			</div>

			{(payment || (recovery && recovery.cart.subtotal > 0)) && (
				<div className="mt-6 rounded-xl border border-zinc-200 p-5">
					<p className="text-sm text-zinc-500">Amount due</p>
					<p className="text-2xl font-bold text-zinc-900">
						{formatINR((payment ? payment.amount : recovery!.cart.subtotal) || 0)}
					</p>
				</div>
			)}

			<div className="mt-6 space-y-3">
				<button
					onClick={startPayment}
					disabled={loading || (recovery ? !recovery.retry_allowed : false)}
					className="w-full rounded-xl bg-black px-5 py-3 font-medium text-white transition hover:bg-zinc-800 disabled:opacity-50"
				>
					{recovery && !recovery.retry_allowed ? "Reservation expired — start a new cart" : "Retry payment"}
				</button>
				<button
					onClick={() => {
						setStep("catalog");
						setCartId(freshCartId());
						setCart(null);
						setOrder(null);
						setPayment(null);
						setRecovery(null);
						setMessage("Choose a new payment method. Your order was not charged.");
					}}
					disabled={loading}
					className="w-full rounded-xl border border-zinc-300 px-5 py-3 font-medium text-zinc-700 hover:bg-zinc-100"
				>
					Change payment method
				</button>
				<button
					onClick={() => {
						setStep("catalog");
						setCartId(freshCartId());
						setCart(null);
						setOrder(null);
						setPayment(null);
						setRecovery(null);
						setMessage("Payment cancelled. Your cart was not charged.");
					}}
					className="w-full rounded-xl border border-zinc-300 px-5 py-3 font-medium text-zinc-700 hover:bg-zinc-100"
				>
					Cancel
				</button>
			</div>
		</section>
	)}

        {step === "complete" && payment && (
          <section>
            <div className="rounded-xl border border-zinc-200 p-6">
              <p className="text-sm text-zinc-500">Payment status</p>
              <p className="mt-1 text-xl font-bold text-zinc-900">
                {payment.status}
              </p>
              <p className="mt-4 text-sm text-zinc-500">
                Payment ID: {payment.payment_id}
              </p>
              <p className="text-sm text-zinc-500">
                Order ID: {payment.order_id}
              </p>
            </div>
            <button
              onClick={() => {
                setStep("catalog");
                setCartId(freshCartId());
                setCart(null);
                setOrder(null);
                setPayment(null);
                setMessage("");
              }}
              className="mt-6 w-full rounded-xl bg-black px-5 py-3.5 font-medium text-white transition hover:bg-zinc-800"
            >
              Start a new order
            </button>
          </section>
        )}

        <p className="mt-8 text-center text-xs text-zinc-400">
          Razorpay Test Mode
        </p>
      </div>
    </main>
  );
}
