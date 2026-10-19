package listings

import (
	"example.com/marketplace/internal/platform/httpx"
	"strings"
	"time"
	"unicode/utf8"
)

type Photo struct {
	ID  string `json:"id"`
	URL string `json:"url,omitempty"`
}
type Listing struct {
	ID          string    `json:"id"`
	OwnerID     string    `json:"owner_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	CategoryID  string    `json:"category_id"`
	Status      string    `json:"status"`
	PriceMinor  int64     `json:"price_minor"`
	Version     int64     `json:"version"`
	Photos      []Photo   `json:"photos"`
	CreatedAt   time.Time `json:"created_at"`
}
type Input struct {
	Title           string `json:"title"`
	Description     string `json:"description"`
	CategoryID      string `json:"category_id"`
	PriceMinor      int64  `json:"price_minor"`
	ExpectedVersion int64  `json:"expected_version"`
}

func CanTransition(from, to string) bool {
	return (from == "draft" || from == "withdrawn") && to == "published" || from == "published" && to == "withdrawn"
}
func Validate(l Listing, publishing bool, photos int) error {
	if utf8.RuneCountInString(l.Title) > 120 || utf8.RuneCountInString(l.Description) > 10000 {
		return httpx.Bad("Название до 120, описание до 10000 символов")
	}
	if l.PriceMinor < 0 || l.PriceMinor > 100000000000 {
		return httpx.Bad("Недопустимая цена")
	}
	if publishing && (strings.TrimSpace(l.Title) == "" || strings.TrimSpace(l.Description) == "" || l.CategoryID == "" || photos == 0) {
		return httpx.Bad("Для публикации заполните название, описание, категорию и добавьте фото")
	}
	return nil
}
