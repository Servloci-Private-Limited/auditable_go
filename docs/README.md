# auditable_go documentation

`auditable_go` records application-level create, update, and delete history for GORM and MongoDB.

## Start here

- [Project explainer and review guide](project-explainer.md) explains the architecture, guarantees, limitations, patterns, and review questions.
- [Initial migration](initial-migration.md) covers the SQL audit and atomic version tables plus MongoDB indexes.
- [Custom diffs and large documents](custom-diff-and-large-documents.md) covers swappable MongoDB diff engines, complexity controls, object storage, and delayed computation.
- [Requirements](auditable.requirements.md) defines the audit event contract.
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
