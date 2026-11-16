-- +goose Up

-- What needs the owner, told once (internal/notify): an autopay that may not cover the card bills it is to
-- take, a document whose statement does not add up or that failed, an account whose balances do not join
-- up or whose source has gone quiet, points about to lapse. Worked out again from the ledger whenever it
-- may have changed. `key` names the situation: while it lasts it is raised once, and a new one is news.
-- `facts` is what was so, as the API shows it.
CREATE TABLE notifications (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind        text        NOT NULL CHECK (kind IN ('shortfall', 'document', 'gap', 'overdue', 'lapsing')),
    key         text        NOT NULL UNIQUE,
    account_id  bigint REFERENCES accounts (id),
    -- A document is deleted once it holds nothing of the ledger; what it was stays in `facts`.
    document_id bigint REFERENCES documents (id) ON DELETE SET NULL,
    facts       jsonb       NOT NULL,
    raised_at   timestamptz NOT NULL DEFAULT now(),
    read_at     timestamptz,
    -- The owner said it is dealt with: for what Finchie cannot see, such as money moved in after the
    -- newest statement.
    handled_at  timestamptz,
    -- It stopped being so.
    resolved_at timestamptz
);
CREATE INDEX notifications_open ON notifications (raised_at DESC) WHERE resolved_at IS NULL;

-- +goose Down
DROP TABLE notifications;
