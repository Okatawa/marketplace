CREATE TABLE outbox(id bigserial PRIMARY KEY,event jsonb NOT NULL,headers jsonb NOT NULL DEFAULT '{}',created_at timestamptz NOT NULL DEFAULT now(),sent_at timestamptz);
CREATE INDEX outbox_pending ON outbox(id) WHERE sent_at IS NULL;
