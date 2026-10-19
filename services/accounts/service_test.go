package accounts

import "testing"

func TestRegistrationValidation(t *testing.T) {
	for _, tc := range []struct {
		name, email, password string
		valid                 bool
	}{{"Анна", "anna@example.com", "strong-password", true}, {"", "a@b.com", "strong-password", false}, {"Anna", "invalid", "strong-password", false}, {"Anna", "a@b.com", "short", false}} {
		if err := ValidateRegistration(tc.name, tc.email, tc.password); (err == nil) != tc.valid {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
}
func TestPasswordHash(t *testing.T) {
	h, err := HashPassword("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "correct-password") || CheckPassword(h, "wrong-password") {
		t.Fatal("password validation incorrect")
	}
}
