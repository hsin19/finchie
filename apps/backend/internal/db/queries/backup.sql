-- name: HasSchema :one
-- Reports whether goose has initialized this database; restore refuses a non-empty database.
SELECT (to_regclass('public.goose_db_version'::text) IS NOT NULL)::boolean AS has_schema;

-- name: HasRestorePending :one
-- Reports whether a restore migrated this database but has not committed yet (ADR 0025).
SELECT (to_regclass('public.backup_restore_pending'::text) IS NOT NULL)::boolean AS pending;

-- name: ListBackupTables :many
-- Every application table in public; excludes goose and River's own tables (recreated by migrations)
-- and restore's in-progress marker.
SELECT tablename::text
FROM pg_catalog.pg_tables
WHERE schemaname = 'public'::text
  AND tablename <> 'goose_db_version'::text
  AND tablename <> 'backup_restore_pending'::text
  AND tablename NOT LIKE 'river\_%'::text
  AND tablename NOT LIKE 'test\_seed\_%'::text
ORDER BY tablename;

-- name: ListTableColumns :many
-- Live columns of a public table in storage order, so future migrations need no backup changes.
SELECT a.attname::text
FROM pg_catalog.pg_attribute a
JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public'::text
  AND c.relname = @table_name::text
  AND a.attnum > 0
  AND NOT a.attisdropped
ORDER BY a.attnum;

-- name: ListForeignKeys :many
-- Foreign keys on the named public tables; restore drops them before COPY and adds them back to validate.
SELECT c.relname::text AS table_name,
       con.conname::text AS constraint_name,
       pg_catalog.pg_get_constraintdef(con.oid)::text AS definition
FROM pg_catalog.pg_constraint con
JOIN pg_catalog.pg_class c ON c.oid = con.conrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public'::text
  AND con.contype = 'f'::"char"
  AND c.relname = ANY(@tables::text[])
ORDER BY c.relname, con.conname;

-- name: ListOwnedSequences :many
-- Identity ('i') and serial ('a') sequences owned by the named public tables.
SELECT t.relname::text AS table_name,
       a.attname::text AS column_name,
       s.relname::text AS sequence_name
FROM pg_catalog.pg_class s
JOIN pg_catalog.pg_depend d ON d.objid = s.oid AND d.deptype IN ('a'::"char", 'i'::"char")
JOIN pg_catalog.pg_class t ON t.oid = d.refobjid
JOIN pg_catalog.pg_namespace n ON n.oid = t.relnamespace
JOIN pg_catalog.pg_attribute a ON a.attrelid = t.oid AND a.attnum = d.refobjsubid
WHERE s.relkind = 'S'::"char"
  AND n.nspname = 'public'::text
  AND t.relname = ANY(@tables::text[])
ORDER BY t.relname, a.attname;

-- name: ListDocumentBlobs :many
-- Every original's hash and size; octet_length reads the TOAST header, not the bytes, so backup can tell
-- a truncated blob file without loading content it already has.
SELECT sha256, octet_length(content)::bigint AS size
FROM documents
ORDER BY id;

-- name: GetDocumentContentByHash :one
SELECT content
FROM documents
WHERE sha256 = @sha256;

-- name: RestoreDocumentContent :execrows
UPDATE documents
SET content = @content
WHERE sha256 = @sha256;

-- name: CountMismatchedDocumentHashes :one
SELECT count(*)::bigint
FROM documents
WHERE sha256 IS DISTINCT FROM sha256(content);
