package httpx

import (
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"io"
	"log/slog"
	"net/http"
	"strconv"
)

type Error struct {
	Status  int               `json:"-"`
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func (e *Error) Error() string { return e.Message }
func Bad(message string) error { return &Error{Status: 400, Code: "validation", Message: message} }
func Fail(status int, code, message string) error {
	return &Error{Status: status, Code: code, Message: message}
}
func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
func Decode(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(dst); e != nil {
		return Bad("Некорректный JSON")
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return Bad("Ожидается один JSON объект")
	}
	return nil
}
func Wrap(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			var e *Error
			var pg *pgconn.PgError
			switch {
			case errors.As(err, &e):
			case errors.Is(err, pgx.ErrNoRows):
				e = &Error{404, "not_found", "Не найдено", nil}
			case errors.As(err, &pg) && pg.Code == "23505":
				e = &Error{409, "conflict", "Запись уже существует", nil}
			case errors.As(err, &pg) && (pg.Code == "22P02" || pg.Code == "23503"):
				e = &Error{400, "validation", "Некорректный идентификатор", nil}
			default:
				slog.ErrorContext(r.Context(), "request failed", "error_type", errorType(err))
				e = &Error{500, "internal", "Не удалось выполнить запрос", nil}
			}
			JSON(w, e.Status, map[string]any{"error": e, "request_id": w.Header().Get("X-Request-ID")})
		}
	}
}
func errorType(e error) string {
	var pg *pgconn.PgError
	if errors.As(e, &pg) {
		return "database_" + pg.Code
	}
	return "internal"
}
func Limit(r *http.Request) (int, error) {
	s := r.URL.Query().Get("limit")
	if s == "" {
		return 20, nil
	}
	n, e := strconv.Atoi(s)
	if e != nil || n < 1 || n > 100 {
		return 0, Bad("limit должен быть от 1 до 100")
	}
	return n, nil
}
func ValidID(s string) bool { _, e := uuid.Parse(s); return e == nil }
