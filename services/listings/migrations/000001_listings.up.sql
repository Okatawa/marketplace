CREATE TABLE categories(id text PRIMARY KEY, name text NOT NULL);
INSERT INTO categories VALUES ('electronics','Электроника'),('home','Для дома'),('sport','Спорт и отдых'),('clothing','Одежда'),('other','Другое');
CREATE TABLE listings(id uuid PRIMARY KEY, owner_id uuid NOT NULL, title text NOT NULL DEFAULT '', description text NOT NULL DEFAULT '', category_id text REFERENCES categories(id), price_minor bigint NOT NULL DEFAULT 0 CHECK(price_minor>=0 AND price_minor<=100000000000), status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','published','withdrawn')), version bigint NOT NULL DEFAULT 1, created_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX listings_owner ON listings(owner_id,id);
CREATE TABLE photos(id uuid PRIMARY KEY,listing_id uuid NOT NULL REFERENCES listings(id),object_key text NOT NULL UNIQUE, status text NOT NULL CHECK(status IN ('pending','ready','deleting')),created_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX photos_listing ON photos(listing_id,status);
