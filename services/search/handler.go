package search

import (
	"encoding/json"
	"example.com/marketplace/internal/platform/config"
	"example.com/marketplace/internal/platform/httpx"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strconv"
)

func New(db *pgxpool.Pool, c config.Config) http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/v1/search", httpx.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		limit, e := httpx.Limit(r)
		if e != nil {
			return e
		}
		q := r.URL.Query()
		page := 1
		if q.Get("page") != "" {
			page, e = strconv.Atoi(q.Get("page"))
			if e != nil || page < 1 || page > 1000 {
				return httpx.Bad("Некорректная страница")
			}
		}
		min, max := int64(0), int64(100000000000)
		if q.Get("min_price") != "" {
			min, e = strconv.ParseInt(q.Get("min_price"), 10, 64)
			if e != nil {
				return httpx.Bad("Некорректная цена")
			}
		}
		if q.Get("max_price") != "" {
			max, e = strconv.ParseInt(q.Get("max_price"), 10, 64)
			if e != nil {
				return httpx.Bad("Некорректная цена")
			}
		}
		if min < 0 || max < min || max > 100000000000 || len(q.Get("q")) > 500 {
			return httpx.Bad("Некорректный запрос или диапазон цены")
		}
		rows, e := db.Query(r.Context(), `SELECT data FROM items WHERE projection_id=(SELECT active_projection FROM settings) AND status='published' AND ($1='' OR document @@ plainto_tsquery('russian',$1)) AND ($2='' OR data->>'category_id'=$2) AND (data->>'price_minor')::bigint BETWEEN $3 AND $4 ORDER BY ts_rank(document,plainto_tsquery('russian',$1)) DESC,(data->>'created_at') DESC,id DESC LIMIT $5 OFFSET $6`, q.Get("q"), q.Get("category_id"), min, max, limit, (page-1)*limit)
		if e != nil {
			return e
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var raw []byte
			if e = rows.Scan(&raw); e != nil {
				return e
			}
			var item map[string]any
			if e = json.Unmarshal(raw, &item); e != nil {
				return e
			}
			if photos, ok := item["photos"].([]any); ok {
				for _, v := range photos {
					if p, ok := v.(map[string]any); ok {
						if id, ok := p["id"].(string); ok {
							p["url"] = "/api/v1/listings/" + item["id"].(string) + "/photos/" + id
						}
					}
				}
			}
			items = append(items, item)
		}
		if e = rows.Err(); e != nil {
			return e
		}
		httpx.JSON(w, 200, map[string]any{"items": items, "page": page, "has_more": len(items) == limit})
		return nil
	}))
	return m
}
