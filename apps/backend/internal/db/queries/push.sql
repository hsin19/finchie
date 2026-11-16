-- name: PushKeys :one
SELECT public_key, private_key FROM push_keys;

-- name: CreatePushKeys :exec
-- A second backend starting at the same moment keeps the first one's pair.
INSERT INTO push_keys (public_key, private_key) VALUES (@public_key, @private_key)
ON CONFLICT DO NOTHING;

-- name: SavePushSubscription :exec
-- A device subscribing again, or with new keys, stays one subscription.
INSERT INTO push_subscriptions (endpoint, p256dh, auth) VALUES (@endpoint, @p256dh, @auth)
ON CONFLICT (endpoint) DO UPDATE SET p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth;

-- name: DeletePushSubscription :exec
DELETE FROM push_subscriptions WHERE endpoint = @endpoint;

-- name: PushSubscriptions :many
SELECT endpoint, p256dh, auth FROM push_subscriptions ORDER BY id;
