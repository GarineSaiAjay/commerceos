package x402

import (
	"net/http"
	"time"
)

// Handler gates exactly one fixed resource behind an x402 challenge --
// "scope it small: one code path, one demo scenario" (item 39). It is
// deliberately standalone: unlike backend/commerce/payment's real
// Razorpay-backed checkout, paying this challenge does not create a
// CommerceOS order, consume a mandate, run through the Policy Engine,
// or write to the audit chain. Wiring x402 into this project's actual
// agentic-commerce flow -- an agent autonomously paying x402
// challenges for a real resource as part of a larger checkout, gated
// by the same Policy Engine every other spend already goes through --
// is future work this stub deliberately does not attempt; see this
// package's own doc comment for why that's a materially bigger
// project than a buildathon-scoped stub.
type Handler struct {
	facilitator  *TestModeFacilitator
	requirements Requirements
	resource     http.HandlerFunc
	now          func() time.Time
}

// NewHandler builds a Handler for one resource. resource is called
// only after a payload verifies against requirements -- it never sees
// a request that hasn't paid.
func NewHandler(
	facilitator *TestModeFacilitator,
	requirements Requirements,
	resource http.HandlerFunc,
) *Handler {
	return &Handler{
		facilitator:  facilitator,
		requirements: requirements,
		resource:     resource,
		now:          time.Now,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sig := r.Header.Get(HeaderPaymentSignature)
	if sig == "" {
		// No payment offered yet -- issue the challenge, exactly the
		// "GET a paywalled resource, get a 402 back" first leg of the
		// handshake.
		h.writeChallenge(w, "")
		return
	}

	payload, err := DecodeHeader[Payload](sig)
	if err != nil {
		http.Error(w, "x402: malformed "+HeaderPaymentSignature+" header: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.facilitator.Verify(payload, h.requirements); err != nil {
		// A payment was attempted but didn't verify -- re-issue the
		// challenge with the reason attached, rather than a bare 402,
		// so a real client (or a judge reading the response) can see
		// exactly what was wrong with the attempt instead of guessing.
		h.writeChallenge(w, err.Error())
		return
	}

	settlementHeader, err := EncodeHeader(Settlement{
		Success:   true,
		Network:   h.requirements.Network,
		Payer:     payload.Payer,
		SettledAt: h.now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		http.Error(w, "x402: encode settlement: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set(HeaderPaymentResponse, settlementHeader)
	h.resource(w, r)
}

func (h *Handler) writeChallenge(w http.ResponseWriter, errMsg string) {
	challenge := Challenge{
		X402Version: ProtocolVersion,
		Error:       errMsg,
		Accepts:     []Requirements{h.requirements},
	}

	encoded, err := EncodeHeader(challenge)
	if err != nil {
		http.Error(w, "x402: encode challenge: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set(HeaderPaymentRequired, encoded)
	w.WriteHeader(http.StatusPaymentRequired)
	_, _ = w.Write([]byte("payment required"))
}
