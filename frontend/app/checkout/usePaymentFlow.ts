"use client";

import { useEffect } from "react";
import { API_BASE } from "../../lib/api";
import { MERCHANT_ID, freshCartId } from "./helpers";
import type {
  Order,
  Payment,
  Cart,
  Mandate,
  Decision,
  ApprovalRequestDetail,
  Recovery,
  RejectionRecoverySuggestion,
  Run,
  Step,
} from "./types";

// Payment/approval/recovery flow -- everything from "Confirm Order" (the
// checkout screen) through a completed or failed payment: mandate ->
// policy proposal -> (optional Level 2/3 human approval gate) ->
// Razorpay Standard Checkout -> verification, plus the two recovery
// paths a rejected/failed attempt can take (remove an item, or accept a
// proactive substitute) and the post-payment audit-trail fetch.
//
// Extracted out of checkout.tsx (PLAN-04-UI-UX-AND-LATENCY.md §A2 follow-
// up -- the item-21 split left real components extracted, but
// checkout.tsx itself had grown back past its pre-split 1,559-line
// baseline; this cluster of functions, ~470 lines, was by far the
// largest single concern still living directly in the component body).
// Deliberately NOT a state-ownership move: every piece of state this
// flow touches (order, payment, step, the approval/gate fields, runId/
// run, recovery, substituteSuggestion, ...) still lives in CheckoutFlow
// exactly as before via its own useState calls -- `step` in particular
// is read by the catalog/cart JSX too, not just this flow, so moving
// its ownership here would only add indirection for no benefit. This
// hook instead takes the CURRENT value of everything it reads and the
// setters for everything it writes as plain parameters, and is called
// unconditionally on every CheckoutFlow render (a plain hook call, not
// inside any condition) -- exactly like these functions being defined
// directly in the component body used to work, since a fresh closure
// over that render's state is formed here on every call in exactly the
// same way. Only the eight functions CheckoutFlow's JSX actually calls
// (startPayment, approveAndPay, rejectApproval, removeAccessoryAndRetry,
// acceptSubstitute, resetToCatalog, approveGateAndPay,
// backToOrderFromGate) are returned; the rest (createPaymentWithLaunch,
// loadRecovery, fetchSubstituteSuggestion, fetchApprovalRequest,
// fetchRun, verifyPayment) are internal-only, called from within this
// cluster exactly as they were before the move.
export interface UsePaymentFlowParams {
  order: Order | null;
  step: Step;
  runId: string;
  run: Run | null;
  approvalRequestId: string;
  approvalSnapshot: ApprovalRequestDetail | null;
  substituteSuggestion: RejectionRecoverySuggestion | null;

  setStep: (step: Step) => void;
  setOrder: (order: Order | null) => void;
  setPayment: (payment: Payment | null) => void;
  setCart: (cart: Cart | null) => void;
  setCartId: (cartId: string) => void;
  setApprovalRequestId: (id: string) => void;
  setApprovalReason: (reason: string) => void;
  setApprovalLevel: (level: number) => void;
  setApprovalSnapshot: (detail: ApprovalRequestDetail | null) => void;
  setGateConfirmed: (confirmed: boolean) => void;
  setGateError: (error: string) => void;
  setLoading: (loading: boolean) => void;
  setMessage: (message: string) => void;
  setRunId: (runId: string) => void;
  setRun: (run: Run | null) => void;
  setRunLoading: (loading: boolean) => void;
  setRecovery: (recovery: Recovery | null) => void;
  setSubstituteSuggestion: (suggestion: RejectionRecoverySuggestion | null) => void;
  setSubstituteSuggestionLoading: (loading: boolean) => void;
  setRemovingVariantId: (variantId: string | null) => void;
  setPolicyRejectionReason: (reason: string) => void;
}

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

export function usePaymentFlow(params: UsePaymentFlowParams) {
  const {
    order,
    step,
    runId,
    run,
    approvalRequestId,
    approvalSnapshot,
    substituteSuggestion,
    setStep,
    setOrder,
    setPayment,
    setCart,
    setCartId,
    setApprovalRequestId,
    setApprovalReason,
    setApprovalLevel,
    setApprovalSnapshot,
    setGateConfirmed,
    setGateError,
    setLoading,
    setMessage,
    setRunId,
    setRun,
    setRunLoading,
    setRecovery,
    setSubstituteSuggestion,
    setSubstituteSuggestionLoading,
    setRemovingVariantId,
    setPolicyRejectionReason,
  } = params;

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
      		if (decision.action_id) setRunId(decision.action_id);

      		// Level 2/3 → durable human approval required before an
      		// authorization is issued.
      if (decision.decision === "PENDING_HUMAN_APPROVAL") {
        if (!decision.approval_request_id) {
          throw new Error("Approval required but no request was created");
        }
        const detail = await fetchApprovalRequest(decision.approval_request_id);
        setApprovalRequestId(decision.approval_request_id);
        setApprovalReason(decision.reason || "This purchase requires operator approval.");
        setApprovalLevel(decision.level);
        setApprovalSnapshot(detail);
        setGateConfirmed(false);
        setGateError("");
        setStep(decision.level >= 3 ? "gate" : "approval");
        setLoading(false);
        return;
      }

      		if (decision.decision !== "APPROVED" || !decision.authorization_id) {
      			// Rejected outright (e.g. over the amount ceiling, or an item
      			// not on the merchant's permitted list): show it as a clear
      			// terminal state instead of leaving this same "Confirm Order /
      			// Pay" screen up with a button that would just fail identically
      			// every time it's clicked again -- no payment was ever
      			// attempted, so "failed"/recovery (which assumes a declined
      			// Razorpay attempt) doesn't apply here.
      			setPolicyRejectionReason(decision.reason || "This purchase was not authorized.");
      			setStep("policy_rejected");
      			setLoading(false);
      			return;
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
      						body: JSON.stringify({ cart_id: order!.cart_id }),
      					});
      					if (!res.ok) throw new Error(await res.text());
      					const decision = (await res.json()) as Decision;
      					if (decision.decision !== "APPROVED" || !decision.authorization_id) {
      						throw new Error(decision.reason || "Approval did not produce an authorization");
      					}
      					if (decision.action_id) setRunId(decision.action_id);
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
      						body: JSON.stringify({ cart_id: order!.cart_id, reason: "cancelled at approval screen" }),
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

			// Remove one removable item (e.g. an accessory) from a failed
			// order, rebuild a fresh smaller cart with catalog-authoritative
			// prices/availability, and re-checkout it server-side. The
			// caller returns to the order screen with the smaller order;
			// clicking Pay there re-proposes to the policy engine on the
			// new total -- policy is never bypassed for the smaller cart.
			async function removeAccessoryAndRetry(variantId: string) {
				if (!order) return;
				setRemovingVariantId(variantId);
				setLoading(true);
				setMessage("Removing item and recomputing your order...");
				try {
					const res = await fetch(`${API_BASE}/orders/${order.order_id}/recovery/remove-item`, {
						method: "POST",
						headers: { "Content-Type": "application/json" },
						body: JSON.stringify({ variant_id: variantId }),
					});
					if (!res.ok) throw new Error(await res.text());
					const newOrder = (await res.json()) as Order;
					setOrder(newOrder);
					setCartId(newOrder.cart_id);
					setPayment(null);
					setRecovery(null);
					setMessage("Item removed. Review your updated order, then pay when ready.");
					setStep("checkout");
				} catch (error) {
					setMessage(error instanceof Error ? error.message : "Could not remove that item");
				} finally {
					setLoading(false);
					setRemovingVariantId(null);
				}
			}

			// Fetch a proactive substitute suggestion (item 33,
			// PLAN-01-AGENTIC-CORE.md section 6) the instant a policy rejection
			// lands on screen (see the useEffect above). Offered ABOVE the
			// manual "remove an item" list below, not instead of it: when no
			// substitute is available -- or this fetch fails -- it quietly
			// stays empty and the existing remove-item fallback is
			// unaffected.
			async function fetchSubstituteSuggestion(orderId: string) {
				setSubstituteSuggestionLoading(true);
				try {
					const res = await fetch(`${API_BASE}/orders/${orderId}/recovery/suggest-substitute`, {
						method: "POST",
					});
					if (res.ok) {
						const data = (await res.json()) as RejectionRecoverySuggestion;
						setSubstituteSuggestion(data.available ? data : null);
					} else {
						setSubstituteSuggestion(null);
					}
				} catch {
					setSubstituteSuggestion(null);
				} finally {
					setSubstituteSuggestionLoading(false);
				}
			}

			// Accept the proactive substitute: swap the over-budget item for
			// the suggested in-budget one and re-checkout server-side.
			// Mirrors removeAccessoryAndRetry above -- rebuild from catalog,
			// re-checkout, land back on the checkout screen where clicking
			// Pay re-proposes the smaller order to the policy engine. Policy
			// is never bypassed: this only replaces line items, it never
			// calls /policy/propose or /payment itself.
			async function acceptSubstitute() {
				if (
					!order ||
					!substituteSuggestion?.available ||
					!substituteSuggestion.replaced_item ||
					!substituteSuggestion.substitute
				) {
					return;
				}
				setLoading(true);
				setMessage("Swapping item and recomputing your order...");
				try {
					const res = await fetch(`${API_BASE}/orders/${order.order_id}/recovery/replace-item`, {
						method: "POST",
						headers: { "Content-Type": "application/json" },
						body: JSON.stringify({
							variant_id: substituteSuggestion.replaced_item.variant_id,
							substitute_product_id: substituteSuggestion.substitute.product_id,
						}),
					});
					if (!res.ok) throw new Error(await res.text());
					const newOrder = (await res.json()) as Order;
					setOrder(newOrder);
					setCartId(newOrder.cart_id);
					setPayment(null);
					setRecovery(null);
					setSubstituteSuggestion(null);
					setMessage("Item swapped. Review your updated order, then pay when ready.");
					setStep("checkout");
				} catch (error) {
					setMessage(error instanceof Error ? error.message : "Could not swap that item");
				} finally {
					setLoading(false);
				}
			}

			async function resetToCatalog(messageText: string) {
      				setStep("catalog");
      				setCartId(freshCartId());
      				setCart(null);
      				setOrder(null);
      				setPayment(null);
      				setApprovalRequestId("");
      				setApprovalLevel(0);
      				setApprovalSnapshot(null);
      				setGateConfirmed(false);
      				setGateError("");
      				setPolicyRejectionReason("");
      				setRunId("");
      				setRun(null);
      				setMessage(messageText);
      			}

  // Fetch the current state of an approval request from the server. Used
  // both to build the confirmation snapshot and, for Level 3, to detect
  // drift immediately before acting on stale data.
  async function fetchApprovalRequest(id: string) {
    const res = await fetch(`${API_BASE}/approval-requests/${id}`, { cache: "no-store" });
    if (!res.ok) throw new Error("Failed to load approval request");
    return (await res.json()) as ApprovalRequestDetail;
  }

  // Level 3 hard gate: re-fetch the approval request immediately before
  // acting. Refuses to proceed if it is no longer PENDING or if the
  // amount/items/merchant/cart/policy version have drifted from what was
  // shown on this screen -- never trust cached state for a hard gate.
  async function approveGateAndPay() {
    if (!approvalSnapshot) return;
    setLoading(true);
    setGateError("");
    setMessage("Re-checking approval request...");
    try {
      const fresh = await fetchApprovalRequest(approvalRequestId);
      if (fresh.status !== "PENDING") {
        throw new Error(`This approval request is now ${fresh.status.toLowerCase()}, not pending. Go back and start over.`);
      }
      const drifted =
        fresh.amount !== approvalSnapshot.amount ||
        fresh.currency !== approvalSnapshot.currency ||
        fresh.merchant !== approvalSnapshot.merchant ||
        fresh.cart_id !== approvalSnapshot.cart_id ||
        fresh.policy_version !== approvalSnapshot.policy_version ||
        fresh.items.join("|") !== approvalSnapshot.items.join("|");
      if (drifted) {
        throw new Error("The order changed since this approval was requested. Go back and start over.");
      }
      setMessage("Approving payment...");
      const res = await fetch(`${API_BASE}/approval-requests/${approvalRequestId}/approve`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ cart_id: fresh.cart_id }),
      });
      if (!res.ok) throw new Error(await res.text());
      const decision = (await res.json()) as Decision;
      if (decision.decision !== "APPROVED" || !decision.authorization_id) {
        throw new Error(decision.reason || "Approval did not produce an authorization");
      }
      if (decision.action_id) setRunId(decision.action_id);
      await createPaymentWithLaunch(decision.authorization_id);
    } catch (error) {
      const msg = error instanceof Error ? error.message : "Approval failed";
      setGateError(msg);
      setMessage("");
      setLoading(false);
    }
  }

  // Return to the order screen after a Level 3 gate is invalidated
  // (drifted or no longer pending) so the buyer can re-propose cleanly.
  function backToOrderFromGate() {
    setStep("checkout");
    setApprovalRequestId("");
    setApprovalReason("");
    setApprovalLevel(0);
    setApprovalSnapshot(null);
    setGateConfirmed(false);
    setGateError("");
    setMessage("That approval request is no longer valid. Review your order and try again.");
  }

  // GET /runs/{id}: the audit trail for the action this checkout actually
  // proposed -- proposed -> risk-assessed -> policy-evaluated -> authorized
  // -- reconstructed from the persisted records, not a separate log. Shown
  // inline on the complete/failed screens (files/AUTH.md).
  async function fetchRun() {
    if (!runId) return;
    setRunLoading(true);
    try {
      const res = await fetch(`${API_BASE}/runs/${runId}`, { cache: "no-store" });
      if (!res.ok) throw new Error("Failed to load audit trail");
      setRun((await res.json()) as Run);
    } catch {
      // The audit trail is a nice-to-have on this screen -- a failure to
      // load it should never block showing the buyer their order result.
    } finally {
      setRunLoading(false);
    }
  }

  // policy_rejected added here alongside complete/failed (item 38, P3,
  // PLAN-06-ADDITIONAL-OPPORTUNITIES.md §4): a rejected action still
  // gets an audit run row (the policy-evaluated step just records a
  // REJECT decision instead of an authorization), and the guided demo
  // walkthrough's final step -- "read the audit trail" -- is meant to
  // be reachable right from the rejection screen a buyer who went
  // over budget just landed on, not only from a successful order.
  useEffect(() => {
    if ((step === "complete" || step === "failed" || step === "policy_rejected") && runId && !run) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      fetchRun();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step, runId]);

  // Fetch the proactive substitute suggestion the moment a policy
  // rejection lands on screen -- order?.order_id as the dependency
  // (not just `step`) means this only re-fires for a genuinely new
  // rejected order, not a re-render of the same one, and not again
  // after acceptSubstitute() below clears substituteSuggestion.
  useEffect(() => {
    if (step === "policy_rejected" && order) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      fetchSubstituteSuggestion(order.order_id);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step, order?.order_id]);

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

  return {
    startPayment,
    approveAndPay,
    rejectApproval,
    removeAccessoryAndRetry,
    acceptSubstitute,
    resetToCatalog,
    approveGateAndPay,
    backToOrderFromGate,
  };
}
