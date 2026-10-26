package main

import (
	"testing"

	"github.com/mygrexit/payback-coupon-activator/internal/activator"
)

func TestWorthReporting(t *testing.T) {
	tests := []struct {
		name        string
		sum         activator.Summary
		failedAbove int
		want        bool
	}{
		{"clean run", activator.Summary{Activated: 5, CookiesSaved: true}, 0, false},
		{"one stuck coupon, default threshold", activator.Summary{Activated: 4, Failed: 1, Remaining: 1, CookiesSaved: true}, 0, true},
		// A stuck coupon is in Failed and Remaining; it must count once.
		{"6 stuck coupons stay below 10", activator.Summary{Failed: 6, Remaining: 6, CookiesSaved: true}, 10, false},
		{"exactly at threshold", activator.Summary{Failed: 10, Remaining: 10, CookiesSaved: true}, 10, false},
		{"above threshold", activator.Summary{Failed: 11, Remaining: 11, CookiesSaved: true}, 10, true},
		// Confirmation timed out, but the reload shows the coupon activated.
		{"failed but activated after all", activator.Summary{Activated: 3, Failed: 2, CookiesSaved: true}, 0, false},
		{"unsaved cookies always count", activator.Summary{Activated: 5}, 10, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := worthReporting(tt.sum, tt.failedAbove); got != tt.want {
				t.Errorf("worthReporting(%+v, %d) = %v, want %v", tt.sum, tt.failedAbove, got, tt.want)
			}
		})
	}
}
