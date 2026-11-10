-- +goose Up

-- The parser instance that last parsed a document (DescribeParsers): a document that failed under an
-- earlier one may parse now, since passwords, the owner's details, and OCR are set when the parser starts.
ALTER TABLE documents ADD COLUMN parser_instance TEXT;

-- +goose Down
ALTER TABLE documents DROP COLUMN parser_instance;
