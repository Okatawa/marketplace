// Package config validates service configuration at startup.
package config

import (
 "fmt"
 "os"
 "strings"
)

type Config struct {
 Service, Addr, DatabaseURL, PublicURL, AccountsURL, ListingsURL string
 JWTPrivate, JWTPublic, ServiceToken string
 KafkaBrokers []string
 S3Endpoint, S3PublicEndpoint, S3Access, S3Secret, S3Bucket string
 SMTPAddr, SMTPFrom string
 Production bool
}
func Env(key, fallback string) string { if value:=os.Getenv(key); value!="" {return value}; return fallback }
func Load(service string)(Config,error){
 c:=Config{Service:service, Addr:Env("HTTP_ADDR",":8080"), DatabaseURL:os.Getenv("DATABASE_URL"), PublicURL:Env("PUBLIC_URL","http://localhost:8080"), AccountsURL:Env("ACCOUNTS_URL","http://accounts:8080"), ListingsURL:Env("LISTINGS_URL","http://listings:8080"), JWTPrivate:os.Getenv("JWT_PRIVATE_KEY"), JWTPublic:os.Getenv("JWT_PUBLIC_KEY"), ServiceToken:os.Getenv("SERVICE_TOKEN"), KafkaBrokers:strings.Split(Env("KAFKA_BROKERS","kafka:9092"),","), S3Endpoint:Env("S3_ENDPOINT","http://storage:9000"),S3PublicEndpoint:Env("S3_PUBLIC_ENDPOINT","http://localhost:9000"),S3Access:os.Getenv("S3_ACCESS_KEY"),S3Secret:os.Getenv("S3_SECRET_KEY"),S3Bucket:Env("S3_BUCKET","photos"),SMTPAddr:Env("SMTP_ADDR","mailpit:1025"),SMTPFrom:Env("SMTP_FROM","marketplace@localhost"),Production:os.Getenv("APP_ENV")=="production"}
 if service!="gateway" && c.DatabaseURL=="" {return c,fmt.Errorf("DATABASE_URL is required for %s",service)}
 if c.JWTPublic=="" {return c,fmt.Errorf("JWT_PUBLIC_KEY is required")}
 if service=="accounts" && c.JWTPrivate=="" {return c,fmt.Errorf("JWT_PRIVATE_KEY is required")}
 if c.ServiceToken=="" {return c,fmt.Errorf("SERVICE_TOKEN is required")}
 return c,nil
}
