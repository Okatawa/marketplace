CREATE TABLE conversations(id uuid PRIMARY KEY,listing_id uuid NOT NULL,buyer_id uuid NOT NULL,seller_id uuid NOT NULL,title text NOT NULL,version bigint NOT NULL DEFAULT 0,updated_at timestamptz NOT NULL DEFAULT now(),UNIQUE(listing_id,buyer_id,seller_id),CHECK(buyer_id<>seller_id));
CREATE INDEX conversations_buyer ON conversations(buyer_id,updated_at);
CREATE INDEX conversations_seller ON conversations(seller_id,updated_at);
CREATE TABLE messages(id uuid PRIMARY KEY,seq bigserial UNIQUE,conversation_id uuid NOT NULL REFERENCES conversations(id),sender_id uuid NOT NULL,body text NOT NULL,idempotency_key text NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(conversation_id,sender_id,idempotency_key));
CREATE TABLE outbox(id bigserial PRIMARY KEY,event jsonb NOT NULL,headers jsonb NOT NULL DEFAULT '{}',created_at timestamptz NOT NULL DEFAULT now(),sent_at timestamptz);
CREATE INDEX outbox_pending ON outbox(id) WHERE sent_at IS NULL;
