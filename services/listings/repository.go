package listings

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const columns = `id::text,owner_id::text,title,description,coalesce(category_id,''),price_minor,status,version,created_at`

func scan(row pgx.Row) (Listing, error) {
	var l Listing
	e := row.Scan(&l.ID, &l.OwnerID, &l.Title, &l.Description, &l.CategoryID, &l.PriceMinor, &l.Status, &l.Version, &l.CreatedAt)
	l.Photos = []Photo{}
	return l, e
}
func load(ctx context.Context, db *pgxpool.Pool, id string) (Listing, error) {
	l, e := scan(db.QueryRow(ctx, "SELECT "+columns+" FROM listings WHERE id=$1", id))
	if e != nil {
		return l, e
	}
	rows, e := db.Query(ctx, "SELECT id::text FROM photos WHERE listing_id=$1 AND status='ready' ORDER BY created_at,id", id)
	if e != nil {
		return l, e
	}
	defer rows.Close()
	for rows.Next() {
		var p Photo
		if e = rows.Scan(&p.ID); e != nil {
			return l, e
		}
		l.Photos = append(l.Photos, p)
	}
	return l, rows.Err()
}

// publicSnapshot intentionally contains only public listing fields, never credentials.
func publicSnapshot(l Listing) json.RawMessage { b, _ := json.Marshal(l); return b }
