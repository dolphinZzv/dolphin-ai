package brain

import (
	"context"
	"fmt"
	"os"
	"time"
)

// SeedSubscriptions creates built-in subscriptions if they don't already exist.
func SeedSubscriptions(ctx context.Context, b *Brain) {
	seedSubs := []Subscription{
		{
			Name:         "watch-soul",
			Description:  "Notify when SOUL.md changes",
			EventPattern: "file.*",
			Filters:      SubscriptionFilter{Path: "SOUL.md"},
			Content:      "SOUL.md has been modified — please check for any needed updates.",
			Enabled:      true,
		},
	}

	for _, sub := range seedSubs {
		existing, err := ReadSubscription(ctx, b, sub.Name)
		if err != nil {
			now := time.Now()
			sub.CreatedAt = now
			sub.UpdatedAt = now
			if writeErr := WriteSubscription(ctx, b, sub); writeErr != nil {
				fmt.Fprintf(os.Stderr, "seed: failed to save subscription %q: %v\n", sub.Name, writeErr)
			} else {
				fmt.Fprintf(os.Stderr, "seed: created subscription %q\n", sub.Name)
			}
		} else {
			fmt.Fprintf(os.Stderr, "seed: subscription %q already exists (enabled=%v)\n", sub.Name, existing.Enabled)
		}
	}
}
