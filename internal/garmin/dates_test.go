package garmin

import (
	"strings"
	"testing"
)

func TestValidateDate(t *testing.T) {
	if err := ValidateDate("d", "2026-02-28"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "2026-2-8", "28-02-2026", "2026-02-30", "yesterday", "2026-02-28T00:00:00"} {
		if err := ValidateDate("d", bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestDateOrToday(t *testing.T) {
	got, err := DateOrToday("d", "")
	if err != nil || got != Today() {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := DateOrToday("d", "nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateRange(t *testing.T) {
	if err := ValidateRange("s", "2026-01-01", "e", "2026-01-28", 28); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRange("s", "2026-01-01", "e", "2026-01-01", 1); err != nil {
		t.Fatal(err)
	}
	err := ValidateRange("s", "2026-01-01", "e", "2026-01-29", 28)
	if err == nil || !strings.Contains(err.Error(), "at most 28") {
		t.Fatalf("expected span error, got %v", err)
	}
	if err := ValidateRange("s", "2026-01-10", "e", "2026-01-01", 0); err == nil {
		t.Fatal("expected ordering error")
	}
	if err := ValidateRange("s", "2026-01-01", "e", "2027-06-01", 0); err != nil {
		t.Fatalf("no limit should allow any span: %v", err)
	}
	if err := ValidateRange("s", "bad", "e", "2026-01-01", 0); err == nil {
		t.Fatal("expected parse error")
	}
}
