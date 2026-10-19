package listings

import "testing"

func TestCanTransition(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		want     bool
	}{{"draft", "published", true}, {"published", "withdrawn", true}, {"withdrawn", "published", true}, {"published", "draft", false}} {
		if got := CanTransition(tc.from, tc.to); got != tc.want {
			t.Fatalf("%s -> %s = %v", tc.from, tc.to, got)
		}
	}
}
func TestPublishedValidation(t *testing.T) {
	l := Listing{Title: "Велосипед", Description: "Хорошее состояние", CategoryID: "sport", PriceMinor: 1500000}
	if err := Validate(l, true, 1); err != nil {
		t.Fatal(err)
	}
	if Validate(l, true, 0) == nil {
		t.Fatal("publication without photo")
	}
	l.PriceMinor = -1
	if Validate(l, false, 1) == nil {
		t.Fatal("negative price")
	}
}
