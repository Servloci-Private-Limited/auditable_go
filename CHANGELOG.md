# Changelog

## v5.1.0

### Changed
- Relicensed from MIT to the [Mozilla Public License 2.0](LICENSE). Modifications to this library's own files must be published under MPL-2.0 when distributed; consuming applications that merely import the module are unaffected. `v5.0.0` and earlier remain available to their recipients under MIT.
- Fixed the `docs-pages` GitHub Actions workflow: `setup-python`'s `cache: pip` needs an explicit `cache-dependency-path` because this repo has no `requirements.txt`/`pyproject.toml`.

### Added
- `docs/how-it-works.md`: a function-by-function, file-referenced anatomy of the GORM and MongoDB code paths for developers.
- Reordered documentation navigation (`mkdocs.yml`, `docs/SUMMARY.md`, `docs/README.md`) into a consistent Overview → How it works → Getting started → Custom diffs → Requirements → Project explainer → Publishing sequence.
- Custom readthedocs theme overrides (`theme-overrides/`) so the generated site's Previous/Next buttons show the destination page's title instead of generic text.

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
