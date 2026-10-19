package listings

import (
	"example.com/marketplace/internal/platform/auth"
	"example.com/marketplace/internal/platform/config"
	"example.com/marketplace/internal/platform/httpx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strings"
)

type Service struct {
	db    *pgxpool.Pool
	cfg   config.Config
	media *Media
}

func New(db *pgxpool.Pool, c config.Config) http.Handler {
	s := &Service{db, c, NewMedia(c)}
	m := http.NewServeMux()
	v, e := auth.FromBase64(c.JWTPublic)
	if e != nil {
		panic(e)
	}
	m.HandleFunc("GET /api/v1/categories", httpx.Wrap(s.categories))
	m.HandleFunc("GET /api/v1/listings/{id}", httpx.Wrap(s.get))
	m.HandleFunc("POST /api/v1/listings", httpx.Wrap(s.create))
	m.HandleFunc("PATCH /api/v1/listings/{id}", httpx.Wrap(s.edit))
	m.HandleFunc("POST /api/v1/listings/{id}/publish", httpx.Wrap(s.transition("published")))
	m.HandleFunc("POST /api/v1/listings/{id}/withdraw", httpx.Wrap(s.transition("withdrawn")))
	m.HandleFunc("GET /api/v1/me/listings", httpx.Wrap(s.mine))
	m.HandleFunc("POST /api/v1/listings/{id}/photos", httpx.Wrap(s.upload))
	m.HandleFunc("DELETE /api/v1/listings/{id}/photos/{photo_id}", httpx.Wrap(s.removePhoto))
	m.HandleFunc("GET /api/v1/listings/{id}/photos/{photo_id}", httpx.Wrap(s.photo))
	root := http.NewServeMux()
	root.Handle("/api/", v.Optional(m))
	root.HandleFunc("GET /internal/v1/listings/export", httpx.Wrap(s.export))
	root.HandleFunc("GET /internal/v1/listings/{id}", httpx.Wrap(s.internalGet))
	return root
}
func (s *Service) categories(w http.ResponseWriter, r *http.Request) error {
	rows, e := s.db.Query(r.Context(), "SELECT id,name FROM categories ORDER BY id")
	if e != nil {
		return e
	}
	defer rows.Close()
	items := []map[string]string{}
	for rows.Next() {
		var id, name string
		if e = rows.Scan(&id, &name); e != nil {
			return e
		}
		items = append(items, map[string]string{"id": id, "name": name})
	}
	if e = rows.Err(); e != nil {
		return e
	}
	httpx.JSON(w, 200, map[string]any{"items": items})
	return nil
}
func (s *Service) create(w http.ResponseWriter, r *http.Request) error {
	owner, e := auth.Require(r)
	if e != nil {
		return e
	}
	var in Input
	if e = httpx.Decode(w, r, &in); e != nil {
		return e
	}
	l := Listing{ID: uuid.NewString(), OwnerID: owner, Title: strings.TrimSpace(in.Title), Description: in.Description, CategoryID: in.CategoryID, PriceMinor: in.PriceMinor, Status: "draft", Version: 1}
	if e = Validate(l, false, 0); e != nil {
		return e
	}
	_, e = s.db.Exec(r.Context(), "INSERT INTO listings(id,owner_id,title,description,category_id,price_minor) VALUES($1,$2,$3,$4,nullif($5,''),$6)", l.ID, owner, l.Title, l.Description, l.CategoryID, l.PriceMinor)
	if e != nil {
		return e
	}
	l, e = load(r.Context(), s.db, l.ID)
	if e != nil {
		return e
	}
	httpx.JSON(w, 201, l)
	return nil
}
func (s *Service) get(w http.ResponseWriter, r *http.Request) error {
	l, e := load(r.Context(), s.db, r.PathValue("id"))
	if e != nil {
		return e
	}
	if l.Status == "draft" && auth.User(r) != l.OwnerID {
		return httpx.Fail(404, "not_found", "Объявление не найдено")
	}
	if e = s.decorate(r.Context(), &l); e != nil {
		return e
	}
	httpx.JSON(w, 200, l)
	return nil
}
func (s *Service) edit(w http.ResponseWriter, r *http.Request) error {
	var in Input
	if e := httpx.Decode(w, r, &in); e != nil {
		return e
	}
	return s.change(w, r, in.ExpectedVersion, func(l *Listing) error {
		l.Title = strings.TrimSpace(in.Title)
		l.Description = in.Description
		l.CategoryID = in.CategoryID
		l.PriceMinor = in.PriceMinor
		return nil
	})
}
func (s *Service) transition(to string) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			ExpectedVersion int64 `json:"expected_version"`
		}
		if e := httpx.Decode(w, r, &in); e != nil {
			return e
		}
		return s.change(w, r, in.ExpectedVersion, func(l *Listing) error {
			if !CanTransition(l.Status, to) {
				return httpx.Fail(409, "state", "Недопустимое изменение статуса")
			}
			l.Status = to
			return nil
		})
	}
}
func (s *Service) change(w http.ResponseWriter, r *http.Request, expected int64, mutate func(*Listing) error) error {
	owner, e := auth.Require(r)
	if e != nil {
		return e
	}
	tx, e := s.db.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	l, e := scan(tx.QueryRow(r.Context(), "SELECT "+columns+" FROM listings WHERE id=$1 FOR UPDATE", r.PathValue("id")))
	if e != nil {
		return e
	}
	if l.OwnerID != owner {
		return httpx.Fail(403, "forbidden", "Это чужое объявление")
	}
	if l.Version != expected {
		return httpx.Fail(409, "version", "Объявление изменилось. Обновите страницу")
	}
	if e = mutate(&l); e != nil {
		return e
	}
	var count int
	if e = tx.QueryRow(r.Context(), "SELECT count(*) FROM photos WHERE listing_id=$1 AND status='ready'", l.ID).Scan(&count); e != nil {
		return e
	}
	if e = Validate(l, l.Status == "published", count); e != nil {
		return e
	}
	l.Version++
	_, e = tx.Exec(r.Context(), "UPDATE listings SET title=$2,description=$3,category_id=nullif($4,''),price_minor=$5,status=$6,version=$7 WHERE id=$1", l.ID, l.Title, l.Description, l.CategoryID, l.PriceMinor, l.Status, l.Version)
	if e != nil {
		return e
	}
	if e = s.emit(r.Context(), tx, l); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	return s.get(w, r)
}
func (s *Service) mine(w http.ResponseWriter, r *http.Request) error {
	owner, e := auth.Require(r)
	if e != nil {
		return e
	}
	return s.list(w, r, "owner_id=$1", owner)
}
func (s *Service) list(w http.ResponseWriter, r *http.Request, filter string, arg any) error {
	limit, e := httpx.Limit(r)
	if e != nil {
		return e
	}
	cursor := r.URL.Query().Get("cursor")
	if cursor != "" && !httpx.ValidID(cursor) {
		return httpx.Bad("Некорректный cursor")
	}
	rows, e := s.db.Query(r.Context(), "SELECT "+columns+" FROM listings WHERE "+filter+" AND ($2='' OR id>NULLIF($2,'')::uuid) ORDER BY id LIMIT $3", arg, cursor, limit)
	if e != nil {
		return e
	}
	defer rows.Close()
	items := []Listing{}
	for rows.Next() {
		l, e := scan(rows)
		if e != nil {
			return e
		}
		items = append(items, l)
	}
	if e = rows.Err(); e != nil {
		return e
	}
	rows.Close()
	for i := range items {
		full, err := load(r.Context(), s.db, items[i].ID)
		if err != nil {
			return err
		}
		items[i] = full
	}
	next := ""
	if len(items) == limit {
		next = items[len(items)-1].ID
	}
	httpx.JSON(w, 200, map[string]any{"items": items, "next_cursor": next})
	return nil
}
func (s *Service) internalGet(w http.ResponseWriter, r *http.Request) error {
	if e := auth.Service(r, s.cfg.ServiceToken); e != nil {
		return e
	}
	l, e := load(r.Context(), s.db, r.PathValue("id"))
	if e != nil {
		return e
	}
	httpx.JSON(w, 200, l)
	return nil
}
func (s *Service) export(w http.ResponseWriter, r *http.Request) error {
	if e := auth.Service(r, s.cfg.ServiceToken); e != nil {
		return e
	}
	q := r.URL.Query()
	q.Set("cursor", q.Get("after_id"))
	r.URL.RawQuery = q.Encode()
	return s.list(w, r, "status <> $1", "draft")
}
