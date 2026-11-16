-- +goose Up

-- Web Push (internal/push). The VAPID key pair the backend signs its pushes with is made at its first start
-- and kept: a device's subscription is tied to the public key it subscribed with, so a new pair would
-- silence every device. Nothing about it is set up by hand.
CREATE TABLE push_keys (
    only_one    boolean PRIMARY KEY DEFAULT true CHECK (only_one),
    public_key  text        NOT NULL,
    private_key text        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Each device that asked for pushes: where its push service takes them, and the keys they are encrypted
-- to. A push service answering that one is gone removes it.
CREATE TABLE push_subscriptions (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    endpoint   text        NOT NULL UNIQUE,
    p256dh     text        NOT NULL,
    auth       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE push_subscriptions;
DROP TABLE push_keys;
