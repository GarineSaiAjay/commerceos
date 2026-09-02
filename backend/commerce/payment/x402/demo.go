package x402

import (
	"encoding/json"
	"net/http"
)

// DemoRequirements is item 39's one fixed scenario: $0.01 of a
// synthetic USDC-on-Base-shaped asset, unlocking a small "priority
// support" resource. The amount/network/asset values are plausible
// but not wired to any real facilitator or chain -- see this
// package's own doc comment. "priority support" was picked as the
// gated resource because it's a genuine, small, agent-payable
// resource shape (an AI agent autonomously paying a merchant's API
// for a real capability), not because this project actually has a
// support system behind it.
func DemoRequirements() Requirements {
	return Requirements{
		Scheme:            "exact",
		Network:           "base-sepolia",
		Asset:             "USDC",
		Amount:            "10000", // $0.01 at USDC's 6 decimals, atomic-unit style
		PayTo:             "0xCommerceOSDemoMerchantAddress0000000001",
		Resource:          "/x402/priority-support",
		Description:       "CommerceOS priority support -- demo resource for item 39 (P3, PLAN-06-ADDITIONAL-OPPORTUNITIES.md §1)",
		MaxTimeoutSeconds: 60,
	}
}

type demoResourceResponse struct {
	Message string `json:"message"`
	Note    string `json:"note"`
}

// DemoResource is the payload served once DemoRequirements' challenge
// has been paid. It never runs for an unpaid request -- Handler only
// calls it after TestModeFacilitator.Verify succeeds.
func DemoResource(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(demoResourceResponse{
		Message: "You now have CommerceOS priority support.",
		Note:    "Unlocked via a test-mode x402 payment -- see backend/commerce/payment/x402's package doc comment for exactly what that does and doesn't mean.",
	})
}
