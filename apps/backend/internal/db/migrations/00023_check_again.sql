-- +goose Up

-- An account with no lines for a while gets no statement, so its newest balance grows old and it reads
-- as overdue though nothing is wrong. The owner can say when to look again: until that day it is not
-- called overdue. Confirming it is current gives the day its kind's rule would; one that never moves
-- can be given a day far off. A statement with a newer balance clears it, however far off (ingest).
ALTER TABLE accounts ADD COLUMN check_again_on date;

-- +goose Down
ALTER TABLE accounts DROP COLUMN check_again_on;
