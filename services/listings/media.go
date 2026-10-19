package listings

import (
	"bytes"
	"context"
	"errors"
	"example.com/marketplace/internal/platform/auth"
	"example.com/marketplace/internal/platform/config"
	"example.com/marketplace/internal/platform/httpx"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"time"
)

type Media struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
}

func NewMedia(c config.Config) *Media {
	newClient := func(endpoint string) *s3.Client {
		return s3.New(s3.Options{Region: "us-east-1", BaseEndpoint: aws.String(endpoint), UsePathStyle: true, Credentials: credentials.NewStaticCredentialsProvider(c.S3Access, c.S3Secret, "")})
	}
	return &Media{newClient(c.S3Endpoint), s3.NewPresignClient(newClient(c.S3PublicEndpoint)), c.S3Bucket}
}
func (m *Media) Ensure(ctx context.Context) error {
	_, e := m.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &m.bucket})
	if e == nil {
		return nil
	}
	_, e = m.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &m.bucket})
	var api smithy.APIError
	if errors.As(e, &api) && (api.ErrorCode() == "BucketAlreadyOwnedByYou") {
		return nil
	}
	return e
}
func ValidateImage(b []byte) (string, error) {
	if len(b) > 5*1024*1024 {
		return "", httpx.Bad("Фотография больше 5 МБ")
	}
	c, format, e := image.DecodeConfig(bytes.NewReader(b))
	if e != nil || (format != "jpeg" && format != "png") || c.Width <= 0 || c.Height <= 0 || int64(c.Width)*int64(c.Height) > 20000000 {
		return "", httpx.Bad("Загрузите JPEG или PNG до 20 млн пикселей")
	}
	if _, _, e = image.Decode(bytes.NewReader(b)); e != nil {
		return "", httpx.Bad("Изображение повреждено")
	}
	return "image/" + format, nil
}
func (m *Media) URL(ctx context.Context, key string) (string, error) {
	o, e := m.presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: &m.bucket, Key: &key}, s3.WithPresignExpires(15*time.Minute))
	if e != nil {
		return "", e
	}
	return o.URL, nil
}
func (s *Service) decorate(ctx context.Context, l *Listing) error {
	for i := range l.Photos {
		var key string
		if e := s.db.QueryRow(ctx, "SELECT object_key FROM photos WHERE id=$1 AND status='ready'", l.Photos[i].ID).Scan(&key); e != nil {
			return e
		}
		u, e := s.media.URL(ctx, key)
		if e != nil {
			return e
		}
		l.Photos[i].URL = u
	}
	return nil
}
func (s *Service) upload(w http.ResponseWriter, r *http.Request) error {
	owner, e := auth.Require(r)
	if e != nil {
		return e
	}
	l, e := load(r.Context(), s.db, r.PathValue("id"))
	if e != nil {
		return e
	}
	if l.OwnerID != owner {
		return httpx.Fail(403, "forbidden", "Это чужое объявление")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 5*1024*1024+64*1024)
	if e = r.ParseMultipartForm(5*1024*1024 + 64*1024); e != nil {
		return httpx.Bad("Не удалось прочитать фото (лимит 5 МБ)")
	}
	defer r.MultipartForm.RemoveAll()
	f, _, e := r.FormFile("file")
	if e != nil {
		return httpx.Bad("Добавьте файл")
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 5*1024*1024+1))
	if e != nil {
		return e
	}
	mime, e := ValidateImage(b)
	if e != nil {
		return e
	}
	id := uuid.NewString()
	key := l.ID + "/" + id
	if _, e = s.db.Exec(r.Context(), "INSERT INTO photos(id,listing_id,object_key,status) VALUES($1,$2,$3,'pending')", id, l.ID, key); e != nil {
		return e
	}
	_, e = s.media.client.PutObject(r.Context(), &s3.PutObjectInput{Bucket: &s.media.bucket, Key: &key, Body: bytes.NewReader(b), ContentLength: aws.Int64(int64(len(b))), ContentType: &mime})
	if e != nil {
		return e
	}
	tx, e := s.db.Begin(r.Context())
	if e != nil {
		return e
	}
	defer tx.Rollback(r.Context())
	l, e = scan(tx.QueryRow(r.Context(), "SELECT "+columns+" FROM listings WHERE id=$1 FOR UPDATE", l.ID))
	if e != nil {
		return e
	}
	var count int
	if e = tx.QueryRow(r.Context(), "SELECT count(*) FROM photos WHERE listing_id=$1 AND status='ready'", l.ID).Scan(&count); e != nil {
		return e
	}
	if count >= 8 {
		return httpx.Bad("Не более 8 фотографий")
	}
	tag, e := tx.Exec(r.Context(), "UPDATE photos SET status='ready' WHERE id=$1 AND status='pending'", id)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return httpx.Fail(409, "upload_expired", "Загрузка устарела, повторите")
	}
	l.Version++
	if _, e = tx.Exec(r.Context(), "UPDATE listings SET version=$2 WHERE id=$1", l.ID, l.Version); e != nil {
		return e
	}
	if e = s.emit(r.Context(), tx, l); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	url, e := s.media.URL(r.Context(), key)
	if e != nil {
		return e
	}
	httpx.JSON(w, 201, Photo{ID: id, URL: url})
	return nil
}
func (s *Service) removePhoto(w http.ResponseWriter, r *http.Request) error {
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
	var n int
	if e = tx.QueryRow(r.Context(), "SELECT count(*) FROM photos WHERE listing_id=$1 AND status='ready'", l.ID).Scan(&n); e != nil {
		return e
	}
	if l.Status == "published" && n <= 1 {
		return httpx.Bad("Опубликованному объявлению нужно хотя бы одно фото")
	}
	tag, e := tx.Exec(r.Context(), "UPDATE photos SET status='deleting' WHERE id=$1 AND listing_id=$2 AND status='ready'", r.PathValue("photo_id"), l.ID)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return httpx.Fail(404, "not_found", "Фото не найдено")
	}
	l.Version++
	if _, e = tx.Exec(r.Context(), "UPDATE listings SET version=$2 WHERE id=$1", l.ID, l.Version); e != nil {
		return e
	}
	if e = s.emit(r.Context(), tx, l); e != nil {
		return e
	}
	if e = tx.Commit(r.Context()); e != nil {
		return e
	}
	w.WriteHeader(204)
	return nil
}
func (s *Service) photo(w http.ResponseWriter, r *http.Request) error {
	l, e := load(r.Context(), s.db, r.PathValue("id"))
	if e != nil {
		return e
	}
	if l.Status == "draft" && auth.User(r) != l.OwnerID {
		return httpx.Fail(404, "not_found", "Фото не найдено")
	}
	var key string
	if e = s.db.QueryRow(r.Context(), "SELECT object_key FROM photos WHERE listing_id=$1 AND id=$2 AND status='ready'", l.ID, r.PathValue("photo_id")).Scan(&key); e != nil {
		return e
	}
	u, e := s.media.URL(r.Context(), key)
	if e != nil {
		return e
	}
	http.Redirect(w, r, u, 302)
	return nil
}
func Cleanup(ctx context.Context, db *pgxpool.Pool, c config.Config) error {
	m := NewMedia(c)
	rows, e := db.Query(ctx, "UPDATE photos SET status='deleting' WHERE status='deleting' OR (status='pending' AND created_at<now()-interval '1 hour') RETURNING id::text,object_key")
	if e != nil {
		return e
	}
	type object struct{ id, key string }
	items := []object{}
	for rows.Next() {
		var o object
		if e = rows.Scan(&o.id, &o.key); e != nil {
			rows.Close()
			return e
		}
		items = append(items, o)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, o := range items {
		if _, e = m.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &m.bucket, Key: &o.key}); e != nil {
			return e
		}
		if _, e = db.Exec(ctx, "DELETE FROM photos WHERE id=$1 AND status='deleting'", o.id); e != nil {
			return e
		}
	}
	return nil
}
