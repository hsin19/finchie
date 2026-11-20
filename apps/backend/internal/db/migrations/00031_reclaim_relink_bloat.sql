-- +goose NO TRANSACTION
-- VACUUM cannot run in a transaction.

-- +goose Up

-- Until SetParents, SetCounterparts, and SetPaid wrote only the links that change, every relink rewrote
-- every link: tens of thousands of dead rows that left transactions dozens of times its size, and every
-- scan of it slower. They no longer pile up; this gives back the space they took, once. It runs as the
-- backend starts, before it serves anything, and takes seconds at this size.
VACUUM (FULL, ANALYZE) transactions, statements;

-- +goose Down
-- Nothing to undo.
