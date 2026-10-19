package config

import "testing"

func TestConfigMissingSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load("accounts"); err == nil {
		t.Fatal("missing database configuration accepted")
	}
}
