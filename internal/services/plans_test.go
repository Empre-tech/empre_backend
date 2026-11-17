package services

import (
	"testing"
	"time"

	"empre_backend/internal/models"
)

func TestSubscriptionIsPro(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	later, earlier := now.Add(24*time.Hour), now.Add(-time.Hour)
	cases := []struct {
		name string
		sub  models.Subscription
		want bool
	}{
		{"activa y al día", models.Subscription{Status: models.SubscriptionActive, CurrentPeriodEnd: &later}, true},
		{"activa pero vencida", models.Subscription{Status: models.SubscriptionActive, CurrentPeriodEnd: &earlier}, false},
		{"activa sin fecha", models.Subscription{Status: models.SubscriptionActive}, false},
		{"inactiva", models.Subscription{Status: models.SubscriptionInactive, CurrentPeriodEnd: &later}, false},
	}
	for _, c := range cases {
		if got := subscriptionIsPro(c.sub, now); got != c.want {
			t.Fatalf("%s: %v", c.name, got)
		}
	}
}
