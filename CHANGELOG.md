# Changelog

## v5.0.0

Initial public release.

### Added
- `postgres-gorm` plugin: atomic per-entity version allocation via `auditable.Migrate`/`MigrateWithTable`, `FailOnAuditError` and `AllowUnauditedBulk` config options, and rejection of conditional bulk writes without a concrete primary key (`ErrBulkMutationUnsupported`).
- `mongodb` and `mongodbv2` packages: pluggable `ChangeComputer` diff engines, `DeferredChanges` handling for oversized/deep documents, configurable `DiffLimits`, and atomic version sequence collections.
- Full documentation set under `docs/`, publishable via GitBook Git Sync (`.gitbook.yaml`, `docs/SUMMARY.md`) and GitHub Pages (`mkdocs.yml`, `.github/workflows/docs-pages.yml`).

### Changed
- Module path updated to `github.com/ivikasavnish/auditable_go/v5`.
- README examples updated to use `mongodbv2` and `auditable.Migrate`.

### Notes
- `go build ./...`, `go vet ./...`, and `go test ./...` pass.
- `mkdocs build --strict` passes.
