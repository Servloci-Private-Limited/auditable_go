# Custom diffs and large documents

MongoDB driver v1 and v2 packages expose the same change-computation design. The concrete `bson.M` type comes from the selected driver major version.

## Swappable change computation

Set `Config.ChangeComputer` to replace the built-in BSON diff logic:

```go
auditor := mongoaudit.NewAuditor(db, "audits", mongoaudit.Config{
    ChangeComputer: mongoaudit.ChangeComputerFunc(
        func(ctx context.Context, req mongoaudit.ChangeRequest) (mongoaudit.ChangeResult, error) {
            // Apply req.Policy before returning values.
            // Check ctx.Done() during expensive traversal.
            changes, err := myJSONDiff(ctx, req.Before, req.After)
            if err != nil {
                return mongoaudit.ChangeResult{}, err
            }
            return mongoaudit.ChangeResult{
                Changes: changes,
                Status:  mongoaudit.DiffInline,
            }, nil
        },
    ),
})
```

`ChangeRequest.Mode` distinguishes creates, operator updates, replacements, and deletes. The request also contains collection/document identity and the effective skip, whitelist, and redaction policy.

The custom computer is invoked synchronously, without library-held locks. It must:

- honor context cancellation and deadlines;
- avoid calling the same audited collection synchronously;
- return only fields permitted by `req.Policy`;
- avoid unbounded recursion and allocation;
- return deterministic output for the same input;
- avoid network calls unless their latency is strictly bounded.

Go cannot forcibly cancel a custom implementation that ignores its context. The library supplies a deadline and rejects a result completed after it, but the implementation must cooperate to bound CPU time.

## Inline complexity limits

`Config.DiffLimits` bounds inline work:

```go
DiffLimits: mongoaudit.DiffLimits{
    MaxDocumentBytes: 2 << 20,
    MaxDepth:         80,
    MaxChangedFields: 2_000,
    ComputeTimeout:   500 * time.Millisecond,
},
```

Defaults are:

| Limit | Default | Purpose |
|---|---:|---|
| `MaxDocumentBytes` | 1 MiB per before/after snapshot | Avoid copying and traversing large documents inline. |
| `MaxDepth` | 64 | Bound deeply nested traversal. |
| `MaxChangedFields` | 1,000 | Prevent oversized inline audit payloads. |
| `ComputeTimeout` | 250 ms | Give custom algorithms a deadline. |

When a limit is exceeded and no deferred handler exists, the audit path returns `ErrChangeTooComplex`. `OnError` is called. When `FailOnAuditError` is enabled, the mutation method also returns the error.

## Object storage and delayed computation

`Config.DeferredChanges` is a storage/provider-neutral boundary. The handler should durably upload snapshots, enqueue a diff job, and return a stable reference:

```go
DeferredChanges: mongoaudit.DeferredChangeHandlerFunc(
    func(ctx context.Context, req mongoaudit.ChangeRequest, reason error) (mongoaudit.ChangeResult, error) {
        objectKey, err := blobStore.PutJSON(ctx, req.DocumentID, req)
        if err != nil {
            return mongoaudit.ChangeResult{}, err
        }
        jobID, err := diffQueue.Enqueue(ctx, objectKey)
        if err != nil {
            return mongoaudit.ChangeResult{}, err
        }
        return mongoaudit.ChangeResult{
            Reference: "audit-diff://" + jobID,
            Status:    mongoaudit.DiffDeferred,
        }, nil
    },
),
```

The audit document stores:

- `diff_status: "deferred"`;
- `changes_ref: "audit-diff://..."`;
- an empty or minimal inline `audited_changes` map.

The library sanitizes exact top-level skipped and redacted fields before invoking the deferred handler. Nested secret classification is still the application's responsibility; use a custom computer/handler with path-aware policy when documents contain nested sensitive values.

The handler must complete the durable upload and enqueue before returning. Starting an untracked goroutine is unsafe: the process can exit after the mutation commits and before the payload is durable.

## Recommended delayed pipeline

```text
application mutation
      |
      v
complexity check
      |
      +-- within limits --> compute inline --> audit event with changes
      |
      +-- over limit ----> sanitize snapshots
                              |
                              v
                        object storage
                              |
                              v
                         durable queue
                              |
                              v
                       background differ
                              |
                              v
                   result keyed by changes_ref
```

Keep the original audit event append-only. Store delayed results as a separate immutable object or a linked enrichment event rather than rewriting the original event in place.

## Deadlock and latency guidance

- No application mutex is held while custom diff or deferred code runs.
- Atomic versions use database-side sequence rows/documents instead of process locks.
- Keep transaction lock order consistent: business row/document, version sequence, audit event.
- Do not perform recursive audited writes from `ChangeComputer`, `DeferredChanges`, `OnAudit`, or `OnError`.
- Keep `OnAudit` and `OnError` local and fast; use a transactional outbox for remote delivery.
- Set context deadlines on the original mutation and every object-store/queue client.
- Bound queue retries and make object keys/job IDs idempotent.
- Monitor compute duration, deferred rate, upload failures, queue age, and unresolved references.

## Consistency caveat

Returning an audit error after a MongoDB mutation does not undo that mutation. Use a MongoDB transaction when the business write, version increment, and audit record must commit or roll back together. GORM-to-Mongo remains a cross-database dual write; use a SQL transactional outbox for strong consistency.
