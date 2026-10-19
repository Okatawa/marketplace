package messaging

import (
	"errors"
	"example.com/marketplace/internal/platform/auth"
	"example.com/marketplace/internal/platform/config"
	"example.com/marketplace/internal/platform/httpx"
	"example.com/marketplace/internal/platform/kafka"
	"example.com/marketplace/internal/platform/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Service struct {
	db  *pgxpool.Pool
	cfg config.Config
}

const convoColumns = `id::text,listing_id::text,buyer_id::text,seller_id::text,title,updated_at`
const messageColumns = `id::text,seq,conversation_id::text,sender_id::text,body,created_at`

func scanConversation(row pgx.Row) (Conversation, error) {
	var c Conversation
	e := row.Scan(&c.ID, &c.ListingID, &c.BuyerID, &c.SellerID, &c.Title, &c.UpdatedAt)
	return c, e
}
func scanMessage(row pgx.Row) (Message, error) {
	var m Message
	e := row.Scan(&m.ID, &m.Seq, &m.ConversationID, &m.SenderID, &m.Body, &m.CreatedAt)
	return m, e
}
func New(db *pgxpool.Pool, c config.Config) http.Handler {
	s := &Service{db, c}
	m := http.NewServeMux()
	m.HandleFunc("POST /api/v1/conversations", httpx.Wrap(s.open))
	m.HandleFunc("GET /api/v1/conversations", httpx.Wrap(s.list))
	m.HandleFunc("GET /api/v1/conversations/{id}/messages", httpx.Wrap(s.messages))
	m.HandleFunc("POST /api/v1/conversations/{id}/messages", httpx.Wrap(s.send))
	v, e := auth.FromBase64(c.JWTPublic)
	if e != nil {
		panic(e)
	}
	return v.Optional(m)
}
func (s *Service) open(w http.ResponseWriter, r *http.Request) error {
	user, e := auth.Require(r)
	if e != nil {
		return e
	}
	var in struct {
		ListingID string `json:"listing_id"`
	}
	if e = httpx.Decode(w, r, &in); e != nil {
		return e
	}
	if !httpx.ValidID(in.ListingID) {
		return httpx.Bad("Некорректное объявление")
	}
	c, e := scanConversation(s.db.QueryRow(r.Context(), "SELECT "+convoColumns+" FROM conversations WHERE listing_id=$1 AND buyer_id=$2", in.ListingID, user))
	if e == nil {
		httpx.JSON(w, 200, c)
		return nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	var listing struct {
		ID            string `json:"id"`
		OwnerID       string `json:"owner_id"`
		Title, Status string
	}
	if e = httpx.GetJSON(r.Context(), s.cfg.ListingsURL+"/internal/v1/listings/"+in.ListingID, s.cfg.ServiceToken, &listing); e != nil {
		return e
	}
	if listing.OwnerID == user {
		return httpx.Bad("Нельзя написать самому себе")
	}
	if listing.Status != "published" {
		return httpx.Fail(409, "unavailable", "Объявление снято с продажи")
	}
	c, e = scanConversation(s.db.QueryRow(r.Context(), "INSERT INTO conversations(id,listing_id,buyer_id,seller_id,title) VALUES($1,$2,$3,$4,$5) ON CONFLICT(listing_id,buyer_id,seller_id) DO UPDATE SET title=conversations.title RETURNING "+convoColumns, uuid.NewString(), in.ListingID, user, listing.OwnerID, listing.Title))
	if e != nil {
		return e
	}
	httpx.JSON(w, 201, c)
	return nil
}
func (s *Service) participant(r *http.Request) (Conversation, string, error) {
	user, e := auth.Require(r)
	if e != nil {
		return Conversation{}, "", e
	}
	c, e := scanConversation(s.db.QueryRow(r.Context(), "SELECT "+convoColumns+" FROM conversations WHERE id=$1", r.PathValue("id")))
	if e != nil {
		return c, user, e
	}
	if user != c.BuyerID && user != c.SellerID {
		return c, user, httpx.Fail(403, "forbidden", "Это чужая переписка")
	}
	return c, user, nil
}
func (s *Service) messages(w http.ResponseWriter, r *http.Request) error {
	c, _, e := s.participant(r)
	if e != nil {
		return e
	}
	limit, e := httpx.Limit(r)
	if e != nil {
		return e
	}
	var after int64
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		after, e = strconv.ParseInt(cursor, 10, 64)
		if e != nil || after < 0 {
			return httpx.Bad("Некорректный cursor")
		}
	}
	rows, e := s.db.Query(r.Context(), "SELECT "+messageColumns+" FROM messages WHERE conversation_id=$1 AND seq>$2 ORDER BY seq LIMIT $3", c.ID, after, limit)
	if e != nil {
		return e
	}
	defer rows.Close()
	items := []Message{}
	for rows.Next() {
		m, e := scanMessage(rows)
		if e != nil {
			return e
		}
		items = append(items, m)
	}
	if e = rows.Err(); e != nil {
		return e
	}
	next := strconv.FormatInt(after, 10)
	if len(items) > 0 {
		next = strconv.FormatInt(items[len(items)-1].Seq, 10)
	}
	httpx.JSON(w, 200, map[string]any{"items": items, "next_cursor": next, "conversation": c})
	return nil
}
func (s *Service) send(w http.ResponseWriter, r *http.Request) error {
	user, e := auth.Require(r)
	if e != nil {
		return e
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) < 1 || len(key) > 128 {
		return httpx.Bad("Нужен Idempotency-Key до 128 символов")
	}
	var in struct {
		Body string `json:"body"`
	}
	if e = httpx.Decode(w, r, &in); e != nil {
		return e
	}
	if strings.TrimSpace(in.Body) == "" || utf8.RuneCountInString(in.Body) > 4000 {
		return httpx.Bad("Сообщение: от 1 до 4000 символов")
	}
	tx, e := s.db.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	c, e := scanConversation(tx.QueryRow(r.Context(), "SELECT "+convoColumns+" FROM conversations WHERE id=$1 FOR UPDATE", r.PathValue("id")))
	if e != nil {
		return e
	}
	if user != c.BuyerID && user != c.SellerID {
		return httpx.Fail(403, "forbidden", "Это чужая переписка")
	}
	existing, e := scanMessage(tx.QueryRow(r.Context(), "SELECT "+messageColumns+" FROM messages WHERE conversation_id=$1 AND sender_id=$2 AND idempotency_key=$3", c.ID, user, key))
	if e == nil {
		if existing.Body != in.Body {
			return httpx.Fail(409, "idempotency", "Этот ключ уже использован для другого сообщения")
		}
		httpx.JSON(w, 200, existing)
		return nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	m, e := scanMessage(tx.QueryRow(r.Context(), "INSERT INTO messages(id,conversation_id,sender_id,body,idempotency_key) VALUES($1,$2,$3,$4,$5) RETURNING "+messageColumns, uuid.NewString(), c.ID, user, in.Body, key))
	if e != nil {
		return e
	}
	var version int64
	if e = tx.QueryRow(r.Context(), "UPDATE conversations SET version=version+1,updated_at=now() WHERE id=$1 RETURNING version", c.ID).Scan(&version); e != nil {
		return e
	}
	recipient := c.SellerID
	if user == recipient {
		recipient = c.BuyerID
	}
	event := kafka.New("message.created", c.ID, version, map[string]string{"message_id": m.ID, "conversation_id": c.ID, "sender_id": user, "recipient_id": recipient})
	if e = outbox.Insert(r.Context(), tx, event); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	httpx.JSON(w, 201, m)
	return nil
}
func (s *Service) list(w http.ResponseWriter, r *http.Request) error {
	user, e := auth.Require(r)
	if e != nil {
		return e
	}
	limit, e := httpx.Limit(r)
	if e != nil {
		return e
	}
	cursor := r.URL.Query().Get("cursor")
	if cursor != "" && !httpx.ValidID(cursor) {
		return httpx.Bad("Некорректный cursor")
	}
	rows, e := s.db.Query(r.Context(), "SELECT "+convoColumns+",coalesce((SELECT body FROM messages WHERE conversation_id=conversations.id ORDER BY seq DESC LIMIT 1),'') FROM conversations WHERE (buyer_id=$1 OR seller_id=$1) AND ($2='' OR (updated_at,id)<(SELECT updated_at,id FROM conversations WHERE id=NULLIF($2,'')::uuid AND (buyer_id=$1 OR seller_id=$1))) ORDER BY updated_at DESC,id DESC LIMIT $3", user, cursor, limit)
	if e != nil {
		return e
	}
	items := []Conversation{}
	for rows.Next() {
		var c Conversation
		if e = rows.Scan(&c.ID, &c.ListingID, &c.BuyerID, &c.SellerID, &c.Title, &c.UpdatedAt, &c.LastMessage); e != nil {
			rows.Close()
			return e
		}
		items = append(items, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for i := range items {
		peer := items[i].SellerID
		if peer == user {
			peer = items[i].BuyerID
		}
		var p struct{ Name string }
		if err := httpx.GetJSON(r.Context(), s.cfg.AccountsURL+"/api/v1/users/"+peer, "", &p); err == nil {
			items[i].PeerName = p.Name
		} else {
			items[i].PeerName = "Пользователь"
		}
	}
	next := ""
	if len(items) == limit {
		next = items[len(items)-1].ID
	}
	httpx.JSON(w, 200, map[string]any{"items": items, "next_cursor": next})
	return nil
}
