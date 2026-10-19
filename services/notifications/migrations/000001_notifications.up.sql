CREATE TABLE inbox(event_id uuid PRIMARY KEY,received_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE jobs(id uuid PRIMARY KEY REFERENCES inbox(event_id),recipient_id uuid NOT NULL,conversation_id uuid NOT NULL,state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','processing','sent','failed')),attempts integer NOT NULL DEFAULT 0,next_attempt timestamptz NOT NULL DEFAULT now(),lease_until timestamptz,lease_owner uuid,last_error text,created_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX jobs_pending ON jobs(next_attempt) WHERE state IN ('pending','processing');
