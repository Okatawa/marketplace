package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevSetupPreservesSecrets(t *testing.T) {
	d := t.TempDir()
	if err := setup(d); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(filepath.Join(d, ".env"))
	if err := setup(d); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(d, ".env"))
	if string(a) != string(b) {
		t.Fatal("setup rotated existing secrets")
	}
	info, _ := os.Stat(filepath.Join(d, ".env"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("secrets readable by others")
	}
}

func TestDevSetupAddsNewSecretsWithoutRotation(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, ".env")
	os.WriteFile(p, []byte("SERVICE_TOKEN=existing\n"), 0600)
	if err := setup(d); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "SERVICE_TOKEN=existing\n") || !strings.Contains(string(b), "GRAFANA_PASSWORD=") {
		t.Fatal("existing secrets must survive config upgrades")
	}
}
