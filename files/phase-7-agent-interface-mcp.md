# Phase 7 — Agent Interface (MCP, Protocol Adapters)

**Prerequisite:** Phase 6 fully verified.

**Status: nearly complete.** The Commerce MCP server, its narrow tool set, the `PaymentAdapter` protocol-adapter interface (Razorpay as the real, wired implementation), and the `explain_decision` tool are all built and verified:

- `create_checkout` alone never results in a Razorpay call (no `payments` row created).
- `execute_authorized_checkout` requires a valid `authorization_id` from `request_authorization` and independently re-verifies it against the `authorizations` table — a forged ID is rejected by the Policy Engine, not the MCP layer.
- No MCP tool calls Razorpay directly (grep of `backend/mcp/` returns zero SDK/`api.razorpay.com` references).
- `explain_decision` returns a real Phase 3-format explanation for a rejected action.
- `PaymentAdapter`/`RazorpayAdapter` is the real wiring point in `main.go` (no longer dead code), with clear extension points for other rails.

## Remaining

An external MCP client (Claude Desktop or the MCP Inspector) has not yet been pointed at `http://localhost:8081/mcp` to call `search_products` + `get_product` from outside this codebase — everything so far has been exercised via `curl`/JSON-RPC. This needs a human at a desktop app to complete; it's the last box to check before calling Phase 7 fully closed.
