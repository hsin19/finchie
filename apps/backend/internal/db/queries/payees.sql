-- name: NamePayee :exec
-- The owner's name for a counterparty; naming it again replaces the name.
INSERT INTO payees (counterparty, name) VALUES (@counterparty, @name)
ON CONFLICT (counterparty) DO UPDATE SET name = EXCLUDED.name;

-- name: ForgetPayee :exec
DELETE FROM payees WHERE counterparty = @counterparty;
