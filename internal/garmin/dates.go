package garmin

import (
	"fmt"
	"time"
)

// DateLayout is the calendar date format the API uses everywhere.
const DateLayout = "2006-01-02"

// Today returns the current local calendar date as YYYY-MM-DD.
func Today() string { return time.Now().Format(DateLayout) }

// ValidateDate checks that s is a YYYY-MM-DD calendar date.
func ValidateDate(name, s string) error {
	if _, err := time.Parse(DateLayout, s); err != nil {
		return fmt.Errorf("%s must be a calendar date in YYYY-MM-DD format, got %q", name, s)
	}
	return nil
}

// DateOrToday validates s, or returns today's date when s is empty.
func DateOrToday(name, s string) (string, error) {
	if s == "" {
		return Today(), nil
	}
	return s, ValidateDate(name, s)
}

// ValidateRange checks both dates and that start is not after end. When
// maxDays > 0 it also enforces an inclusive span limit.
func ValidateRange(startName, start, endName, end string, maxDays int) error {
	s, err := time.Parse(DateLayout, start)
	if err != nil {
		return fmt.Errorf("%s must be a calendar date in YYYY-MM-DD format, got %q", startName, start)
	}
	e, err := time.Parse(DateLayout, end)
	if err != nil {
		return fmt.Errorf("%s must be a calendar date in YYYY-MM-DD format, got %q", endName, end)
	}
	if e.Before(s) {
		return fmt.Errorf("%s (%s) must not be before %s (%s)", endName, end, startName, start)
	}
	if maxDays > 0 {
		days := int(e.Sub(s).Hours()/24) + 1
		if days > maxDays {
			return fmt.Errorf("date range spans %d days but this endpoint allows at most %d; split the request into smaller ranges", days, maxDays)
		}
	}
	return nil
}
