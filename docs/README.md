# auditable_go documentation

`auditable_go` records application-level create, update, and delete history for GORM and MongoDB.

## Start here

- [How it works (full anatomy)](how-it-works.md) walks the implementation function-by-function, with file references, for developers who want to know exactly what happens on each call.
- [Initial migration](initial-migration.md) covers the SQL audit and atomic version tables plus MongoDB indexes.
- [Custom diffs and large documents](custom-diff-and-large-documents.md) covers swappable MongoDB diff engines, complexity controls, object storage, and delayed computation.
- [Requirements](auditable.requirements.md) defines the audit event contract.
- [Project explainer and review guide](project-explainer.md) explains guarantees, limitations, patterns, and review questions.
- [Publishing with GitBook](publishing.md) describes the Git Sync handoff.

## Package choice

| Application stack | Package |
|---|---|
| GORM with SQL or a custom audit store | `github.com/ivikasavnish/auditable_go/v5/postgres-gorm` |
| MongoDB Go driver v1 | `github.com/ivikasavnish/auditable_go/v5/mongodb` |
| MongoDB Go driver v2 | `github.com/ivikasavnish/auditable_go/v5/mongodbv2` |

The module-root GORM plugin is retained for compatibility. New integrations should use `postgres-gorm`.

## Verification

```bash
go test ./...
go vet ./...
```

Both commands should pass before documentation is published.

## License

Licensed under the [Mozilla Public License 2.0](https://github.com/ivikasavnish/auditable_go/blob/v5/LICENSE). You may use this module freely, including in proprietary applications; modifications to this library's own files must be published under MPL-2.0 when distributed. Releases before this change (`v5.0.0`) remain available under MIT.
