package listings

import (
	"context"
	"example.com/marketplace/internal/platform/kafka"
	"example.com/marketplace/internal/platform/outbox"
	"github.com/jackc/pgx/v5"
)

func (s *Service) emit(ctx context.Context, tx pgx.Tx, l Listing) error {
	if l.Status == "draft" {
		return nil
	}
	rows, e := tx.Query(ctx, "SELECT id::text FROM photos WHERE listing_id=$1 AND status='ready' ORDER BY created_at,id", l.ID)
	if e != nil {
		return e
	}
	l.Photos = []Photo{}
	for rows.Next() {
		var p Photo
		if e = rows.Scan(&p.ID); e != nil {
			rows.Close()
			return e
		}
		l.Photos = append(l.Photos, p)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	return outbox.Insert(ctx, tx, kafka.New("listing.changed", l.ID, l.Version, l))
}
