// Package push sends Web Push messages to the devices that asked for them: the installed app on a phone,
// a browser on a desktop. Nothing is set up: the backend makes its VAPID key pair at its first start and
// keeps it, a device subscribes from the app, and one its push service says is gone is dropped. What a
// message says is the app's to word: its service worker shows it.
package push

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
)

// subscriber is who sends the pushes, as push services ask (VAPID's `sub`): somewhere to reach the
// software's maintainer, not the deployment's owner.
const subscriber = "https://github.com/hsin19/finchie"

// ttl is how long a push service keeps a message for a device that is off: a day, after which what it
// says is in the app anyway.
const ttl = 24 * 60 * 60

// Client is what messages are sent with; tests swap it for one that trusts their server.
var Client = &http.Client{Timeout: 15 * time.Second}

// Keys returns the VAPID key pair, made on first use and kept from then on.
func Keys(ctx context.Context, queries *dbq.Queries) (dbq.PushKeysRow, error) {
	keys, err := queries.PushKeys(ctx)
	if !errors.Is(err, pgx.ErrNoRows) {
		if err != nil {
			return keys, fmt.Errorf("push keys: %w", err)
		}
		return keys, nil
	}
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return keys, fmt.Errorf("make push keys: %w", err)
	}
	if err := queries.CreatePushKeys(ctx, dbq.CreatePushKeysParams{PublicKey: public, PrivateKey: private}); err != nil {
		return keys, fmt.Errorf("store push keys: %w", err)
	}
	keys, err = queries.PushKeys(ctx)
	if err != nil {
		return keys, fmt.Errorf("push keys: %w", err)
	}
	return keys, nil
}

// ErrNotHTTPS means a subscription's endpoint is not a push service's: every browser's is HTTPS.
var ErrNotHTTPS = errors.New("a push endpoint is an https URL")

// Subscribe keeps a device's subscription, or its new keys.
func Subscribe(ctx context.Context, queries *dbq.Queries, endpoint, p256dh, auth string) error {
	if u, err := url.Parse(endpoint); err != nil || u.Scheme != "https" || u.Host == "" {
		return ErrNotHTTPS
	}
	if err := queries.SavePushSubscription(ctx, dbq.SavePushSubscriptionParams{Endpoint: endpoint, P256dh: p256dh, Auth: auth}); err != nil {
		return fmt.Errorf("save push subscription: %w", err)
	}
	return nil
}

// Send delivers each message to every device subscribed, and returns how many deliveries the push
// services took. A device whose push service says it is gone is dropped; any other failure is returned
// once every delivery was tried.
func Send(ctx context.Context, pool *pgxpool.Pool, messages ...[]byte) (int, error) {
	queries := dbq.New(pool)
	subscriptions, err := queries.PushSubscriptions(ctx)
	if err != nil || len(subscriptions) == 0 || len(messages) == 0 {
		if err != nil {
			return 0, fmt.Errorf("push subscriptions: %w", err)
		}
		return 0, nil
	}
	keys, err := Keys(ctx, queries)
	if err != nil {
		return 0, err
	}
	options := &webpush.Options{
		HTTPClient: Client, Subscriber: subscriber, TTL: ttl, Urgency: webpush.UrgencyHigh,
		VAPIDPublicKey: keys.PublicKey, VAPIDPrivateKey: keys.PrivateKey,
	}

	sent := 0
	var errs []error
	for _, s := range subscriptions {
		subscription := &webpush.Subscription{Endpoint: s.Endpoint, Keys: webpush.Keys{P256dh: s.P256dh, Auth: s.Auth}}
		// The endpoint is a capability URL: only its host is logged.
		host := hostOf(s.Endpoint)
		for _, message := range messages {
			status, err := deliver(ctx, message, subscription, options)
			if err != nil {
				errs = append(errs, fmt.Errorf("push to %s: %w", host, err))
				continue
			}
			if status == http.StatusNotFound || status == http.StatusGone {
				slog.InfoContext(ctx, "dropped a push subscription its service says is gone", "host", host, "status", status)
				if err := queries.DeletePushSubscription(ctx, s.Endpoint); err != nil {
					errs = append(errs, fmt.Errorf("drop push subscription: %w", err))
				}
				break
			}
			if status < 200 || status >= 300 {
				errs = append(errs, fmt.Errorf("push to %s: the push service answered %d", host, status))
				continue
			}
			sent++
		}
	}
	return sent, errors.Join(errs...)
}

func deliver(ctx context.Context, message []byte, s *webpush.Subscription, options *webpush.Options) (int, error) {
	response, err := webpush.SendNotificationWithContext(ctx, message, s, options)
	if err != nil {
		return 0, fmt.Errorf("send: %w", err)
	}
	if err := response.Body.Close(); err != nil {
		return 0, fmt.Errorf("close response: %w", err)
	}
	return response.StatusCode, nil
}

func hostOf(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil {
		return u.Host
	}
	return "?"
}
