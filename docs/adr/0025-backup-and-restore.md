# ADR 0025: Backup and restore

## Decision

The backend exposes `backup` and `restore` subcommands that write and read local directories, separating immutable document originals (`documents.content`) from the rest of the database. Encryption, scheduling, and offsite storage stay outside Finchie, in the deployment.

| | |
|---|---|
| Commands | `/backend backup --out <snapshot-dir> --blobs <blobs-dir>` and `/backend restore --in <snapshot-dir> --blobs <blobs-dir>`, reading `BACKEND_DATABASE_URL` (or `--database-url`) |
| Blobs | `<blobs-dir>/<sha256>` (64-char lowercase hex), written through a synced temp file, a rename, and a synced directory, so a crash leaves a blob absent or whole. Each backup extracts only originals not already present in `<blobs-dir>`, verified against their SHA-256; a present blob whose size differs from `octet_length(content)` is extracted again (`repaired_blobs`) rather than skipped by every later backup. Table dumps are synced before `manifest.json` is written |
| Snapshot | `<snapshot-dir>/tables/<table>.copy.gz` (gzipped Postgres `COPY` text for every application table, omitting `goose_db_version` and `river_*`), `<snapshot-dir>/blobs.sha256` (sorted unique hashes, one per line, for deployment scripts to sync without parsing JSON), and `<snapshot-dir>/manifest.json` (format version, goose `schema_version`, table row counts, blob list), written last to mark a complete snapshot |
| Consistency | All tables and missing blobs are read inside a single `REPEATABLE READ READ ONLY` transaction, so every file in a snapshot reflects the same point in time. `documents` metadata (`status`, `duplicate_of`, errors, warnings) stays in `tables/documents.copy.gz` with `content` replaced by empty bytes |
| Restore | Validates every snapshot file and every blob's SHA-256 before touching the database, refuses a non-empty database, creates the marker table `backup_restore_pending`, migrates to the snapshot's `schema_version` (refusing snapshots newer than the binary with `ErrSchemaNewer`), then in one transaction drops foreign keys, truncates migration-seeded rows, loads all tables via `COPY`, reattaches every document original, asserts `sha256 = sha256(content)` in Postgres, syncs identity sequences, adds all foreign keys back so Postgres validates every reference, and drops the marker |
| Retry | goose commits each migration on its own, so a restore that fails after migrating leaves a schema behind. The marker tells it from a database in use: a database holding it is accepted by `restore` again (its transaction truncates every table, so a retry starts from the same state) as long as its schema version still equals the snapshot's, and refused by `backup` (`ErrRestorePending`), so an empty half-restored database never rotates good snapshots out of retention. Restore before the backend first starts on that database: the backend migrates to its newest version at startup |

ADR 0005 keeps originals inside Postgres (`documents.content`) so runtime stays a single database with transactional writes and no extra volumes. Daily `pg_dump` backups, however, repeat every unchanged PDF on every run: a 150 MB database uploaded daily over a home uplink hits timeouts and multiplies offsite storage by the retention window. Keeping the split logic inside the backend instead of in deployment scripts removes cross-repository schema coupling and lets the backend's own integration tests guard backup and restore on every migration.

## Alternatives considered

- **Move `documents.content` out of Postgres onto a local volume.** Makes `pg_dump` tiny automatically, but gives up ADR 0005's single transactional store, adds a mounted volume and migration for existing rows, and introduces orphan-file handling on write failures or deletions.
- **Split `documents.content` in the deployment's backup scripts.** Works without touching Finchie, but couples external shell scripts to Finchie's internal table and column names and cannot be tested by Finchie's CI.
- **Ship `pg_dump` / `pg_restore` or R2 credentials inside the backend.** Ties the distroless image to a specific Postgres client major version or to one cloud provider, whereas local directories work for any deployment and any offsite store.
