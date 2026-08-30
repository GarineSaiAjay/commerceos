-- +goose Up
-- agent_conversations backs PLAN-01 §3's conversation memory for the
-- in-app shopping agent: conversation_id is deliberately the cart_id
-- (a buyer's cart already anchors their session, so no new identity
-- system is needed) and each row is one turn. tool_calls is jsonb so it
-- can hold either shape that ever needs storing here: today (before
-- PLAN-01 §2's bounded tool-calling loop exists) it holds a lightweight
-- merged-Intent state snapshot on user turns, letting a follow-up
-- prompt ("no, for my brother instead") be understood against what the
-- buyer already said instead of failing validation from scratch; once
-- the real tool-calling loop ships, the same column holds actual
-- tool-call records without any migration changes.
CREATE TABLE agent_conversations (
    id BIGSERIAL PRIMARY KEY,
    cart_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    tool_calls JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_conversations_cart_id_created_at
    ON agent_conversations (cart_id, created_at);

-- +goose Down
DROP TABLE agent_conversations;
