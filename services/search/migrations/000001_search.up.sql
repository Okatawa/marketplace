CREATE TABLE projections(id text PRIMARY KEY,group_id text NOT NULL UNIQUE,ready boolean NOT NULL DEFAULT false);
INSERT INTO projections(id,group_id,ready) VALUES('default','search-v1',true);
CREATE TABLE settings(id boolean PRIMARY KEY DEFAULT true CHECK(id),active_projection text NOT NULL REFERENCES projections(id));
INSERT INTO settings VALUES(true,'default');
CREATE TABLE items(projection_id text NOT NULL REFERENCES projections(id),id uuid NOT NULL,version bigint NOT NULL,status text NOT NULL,data jsonb NOT NULL,document tsvector GENERATED ALWAYS AS (to_tsvector('russian',coalesce(data->>'title','')||' '||coalesce(data->>'description',''))) STORED,PRIMARY KEY(projection_id,id));
CREATE INDEX items_document ON items USING gin(document);
