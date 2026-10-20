// devsetup creates local-only credentials. It never rotates existing credentials.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func secret() string {
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func setup(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	p := filepath.Join(dir, ".env")
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return err
		}
		defer f.Close()
		for _, key := range []string{"DEMO_PASSWORD", "GRAFANA_PASSWORD"} {
			if !strings.Contains("\n"+string(raw), "\n"+key+"=") {
				if _, err := fmt.Fprintf(f, "\n%s=%s\n", key, secret()); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "POSTGRES_PASSWORD=%s\nAPP_DB_PASSWORD=%s\nJWT_PRIVATE_KEY=%s\nJWT_PUBLIC_KEY=%s\nSERVICE_TOKEN=%s\nS3_ACCESS_KEY=marketplace\nS3_SECRET_KEY=%s\nDEMO_PASSWORD=%s\nGRAFANA_PASSWORD=%s\n", secret(), secret(), base64.StdEncoding.EncodeToString(priv), base64.StdEncoding.EncodeToString(pub), secret(), secret(), secret(), secret())
	return err
}
func main() {
	if err := setup(".local"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Local configuration ready: .local/.env")
}
