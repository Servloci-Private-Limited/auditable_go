package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	mongoaudit "github.com/ivikasavnish/auditable_go/v5/mongodbv2"
)

// --------------------------------------------------------------------------
// Models
//
// Embed mongoaudit.Model with `bson:",inline"` — you get _id, created_at,
// updated_at, deleted_at and full audit tracking automatically.
//
// `auditable` struct tags (identical to the GORM package):
//
//   auditable:"true"    whitelist: only tagged fields appear in audit entries
//   auditable:"redact"  value stored as [REDACTED]
//   auditable:"-"       field is never recorded
//   (no tag)            always recorded, unless another field has "true"
//
// CRUD op gating on the embedded Model field (MongoDB only):
//
//   mongoaudit.Model `bson:",inline" auditable:"create,update"`
//   → delete operations produce no audit entry for this collection.
//   Use auditor.CollectionFor("col", &T{}) to apply the gate.
// --------------------------------------------------------------------------

// Voter — auditable:"create,update" on the embedded Model means delete is NOT
// audited. auditable:"true" whitelists a field. auditable:"-" skips it entirely.
type Voter struct {
	mongoaudit.Model `bson:",inline" auditable:"create,update"`
	Name             string `bson:"name"`
	EpicNumber       string `bson:"epic_number" auditable:"redact"` // stored as [REDACTED]
	Age              int    `bson:"age"         auditable:"true"`   // whitelist: only audited field
	Status           string `bson:"status"      auditable:"-"`      // never recorded
}

// Campaign — no CRUD gate (all ops audited). auditable:"true" whitelists fields.
type Campaign struct {
	mongoaudit.Model `bson:",inline"`
	Name             string `bson:"name"        auditable:"true"`
	Status           string `bson:"status"      auditable:"true"`
	ProgramID        int    `bson:"program_id"` // not audited — no "true" tag
}

// Category — whitelist: only Name and Slug are audited. DisplayOrder is silently
// ignored. All ops (create, update, delete) are audited.
type Category struct {
	mongoaudit.Model `bson:",inline"`
	Name             string `bson:"name"          auditable:"true"`
	Slug             string `bson:"slug"          auditable:"true"`
	DisplayOrder     int    `bson:"display_order"` // not audited — no "true" tag
}

// Product — full audit; APIKey is redacted, InternalSKU is never recorded.
// Demonstrates ReplaceOne: the full before/after diff is captured.
type Product struct {
	mongoaudit.Model `bson:",inline"`
	Name             string  `bson:"name"`
	Price            float64 `bson:"price"`
	CategoryID       string  `bson:"category_id"`
	APIKey           string  `bson:"api_key"      auditable:"redact"` // stored as [REDACTED]
	InternalSKU      string  `bson:"internal_sku" auditable:"-"`      // never recorded
}

// Tag — full audit, no special rules. Demonstrates batch soft-delete.
type Tag struct {
	mongoaudit.Model `bson:",inline"`
	Name             string `bson:"name"`
	ArticleID        string `bson:"article_id"`
}

// --------------------------------------------------------------------------
// Context key — set once by your auth middleware, read by the auditor.
// --------------------------------------------------------------------------

type ctxKey string

const userKey ctxKey = "current_user"

func withUser(ctx context.Context, id any) context.Context {
	return context.WithValue(ctx, userKey, id)
}

// --------------------------------------------------------------------------
// main
// --------------------------------------------------------------------------

func main() {
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		uri = "mongodb://localhost:27025"
	}
	dbName := os.Getenv("MONGO_DB")
	if dbName == "" {
		dbName = "auditdemo"
	}

	ctx := context.Background()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatal("connect:", err)
	}
	defer client.Disconnect(ctx)

	// db is the *mongo.Database supplied by the caller (here via MONGO_DB env var).
	// Pass any *mongo.Database you already have — NewAuditor does not create its own connection.
	db := client.Database(dbName)

	// Register the auditor once for the database.
	// OnAudit fires synchronously after every write — use it to forward events
	// to a log, metrics system, or message bus.
	auditor := mongoaudit.NewAuditor(db, "audits", mongoaudit.Config{
		UserIDResolver: func(ctx context.Context) (string, bool) {
			return mongoaudit.ExtractUserID(ctx, userKey)
		},
		OnAudit: func(a *mongoaudit.Audit) {
			fmt.Printf("  [audit] %-10s %-8s v%d\n", a.AuditableType, a.Action, a.Version)
		},
	})
	if err := auditor.EnsureIndexes(ctx); err != nil {
		log.Fatal("audit indexes:", err)
	}

	voters := auditor.CollectionFor("voters", &Voter{})          // CRUD ops gated by Voter's Model tag
	campaigns := auditor.CollectionFor("campaigns", &Campaign{}) // all ops (no gate)
	categories := auditor.CollectionFor("categories", &Category{})
	products := auditor.CollectionFor("products", &Product{})
	tags := auditor.CollectionFor("tags", &Tag{})

	// ── Voter (user-1) ────────────────────────────────────────────────────

	ctx1 := withUser(ctx, "user-1")

	v := &Voter{Name: "Ravi Kumar", EpicNumber: "EPIC001", Age: 42, Status: "active"}
	res, err := voters.InsertOne(ctx1, v) // EpicNumber stored as [REDACTED]
	if err != nil {
		log.Fatal(err)
	}
	voterID := res.InsertedID.(bson.ObjectID)

	voters.UpdateOne(ctx1,
		bson.M{"_id": voterID},
		bson.M{"$set": bson.M{"age": 43}},
	)

	// delete NOT audited — gated by auditable:"create,update" on Voter.Model
	voters.DeleteOne(ctx1, bson.M{"_id": voterID})

	// ── Campaign (user-2) ─────────────────────────────────────────────────
	// Switching user mid-session: just use a different context.

	ctx2 := withUser(ctx, "user-2")

	c := &Campaign{Name: "Door-to-Door Drive", Status: "draft", ProgramID: 10}
	res, err = campaigns.InsertOne(ctx2, c) // only Name+Status audited; ProgramID skipped
	if err != nil {
		log.Fatal(err)
	}
	campaignID := res.InsertedID.(bson.ObjectID)

	campaigns.UpdateOne(ctx2,
		bson.M{"_id": campaignID},
		bson.M{"$set": bson.M{"status": "active"}},
	)

	// Hard-delete campaign — all ops audited for Campaign (no CRUD gate).
	campaigns.DeleteOne(ctx2, bson.M{"_id": campaignID})

	// ── Category: soft delete + query + restore (user-1) ─────────────────
	//
	// MongoDB has no built-in soft-delete; the collection provides
	// SoftDelete / SoftDeleteMany / Restore helpers that set/unset deleted_at.
	// Use NotDeleted(filter) to exclude soft-deleted docs in any query.

	cat := &Category{Name: "Technology", Slug: "technology", DisplayOrder: 1}
	res, err = categories.InsertOne(ctx1, cat)
	if err != nil {
		log.Fatal(err)
	}
	catID := res.InsertedID.(bson.ObjectID)

	categories.UpdateOne(ctx1,
		bson.M{"_id": catID},
		bson.M{"$set": bson.M{"name": "Tech", "slug": "tech"}},
	)

	// Soft-delete: sets deleted_at, audited as "update".
	categories.SoftDelete(ctx1, bson.M{"_id": catID})

	// NotDeleted wraps any filter with { deleted_at: { $exists: false } }.
	visible, _ := categories.CountDocuments(ctx1, mongoaudit.NotDeleted(bson.M{}))
	fmt.Printf("\nVisible categories after soft-delete: %d\n", visible) // 0

	allCats, _ := categories.CountDocuments(ctx1, bson.M{})
	fmt.Printf("All categories (including deleted): %d\n", allCats) // 1

	// Restore: unsets deleted_at, audited as "update".
	categories.Restore(ctx1, bson.M{"_id": catID})
	visible, _ = categories.CountDocuments(ctx1, mongoaudit.NotDeleted(bson.M{}))
	fmt.Printf("Visible categories after restore: %d\n", visible) // 1

	// ── Product: ReplaceOne + redaction + soft delete (user-2) ───────────
	//
	// ReplaceOne captures a full before/after diff. APIKey is redacted in both
	// snapshots; InternalSKU never appears in the audit trail.

	prod := &Product{
		Name:        "Widget Pro",
		Price:       49.99,
		CategoryID:  catID.Hex(),
		APIKey:      "sk-live-secret123",
		InternalSKU: "WDG-001",
	}
	res, err = products.InsertOne(ctx2, prod) // APIKey → [REDACTED], InternalSKU absent
	if err != nil {
		log.Fatal(err)
	}
	prodID := res.InsertedID.(bson.ObjectID)

	// ReplaceOne: swaps the whole document; diff shows changed fields only.
	products.ReplaceOne(ctx2,
		bson.M{"_id": prodID},
		&Product{
			Name:        "Widget Pro (v2)",
			Price:       39.99,
			CategoryID:  catID.Hex(),
			APIKey:      "sk-live-newkey999", // still stored as [REDACTED]
			InternalSKU: "WDG-002",           // still never recorded
		},
	)

	// Soft-delete; [REDACTED] stays in log, InternalSKU never appears.
	products.SoftDelete(ctx2, bson.M{"_id": prodID})

	// ── Tag: batch soft-delete with SoftDeleteMany (user-1) ──────────────
	//
	// SoftDeleteMany runs one UpdateMany under the hood; one audit entry is
	// emitted per affected document.

	articleID := bson.NewObjectID()

	tagDocs := []interface{}{
		&Tag{Name: "go", ArticleID: articleID.Hex()},
		&Tag{Name: "audit", ArticleID: articleID.Hex()},
		&Tag{Name: "mongodb", ArticleID: articleID.Hex()},
	}
	tags.InsertMany(ctx1, tagDocs)

	tags.SoftDeleteMany(ctx1, bson.M{"article_id": articleID.Hex()})

	remaining, _ := tags.CountDocuments(ctx1, mongoaudit.NotDeleted(bson.M{"article_id": articleID.Hex()}))
	fmt.Printf("\nVisible tags after batch soft-delete: %d\n", remaining) // 0

	// Hard-delete one tag permanently — audited as "delete".
	tags.DeleteOne(ctx1, bson.M{"article_id": articleID.Hex()})

	// ── Audit trail ───────────────────────────────────────────────────────

	cursor, err := auditor.AuditCollection().Find(ctx, bson.M{}, options.Find().SetSort(bson.M{"_id": 1}))
	if err != nil {
		log.Fatal(err)
	}
	var trail []mongoaudit.Audit
	if err := cursor.All(ctx, &trail); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\n%-12s  %-8s  %-5s  %-8s  %s\n", "type", "action", "ver", "user", "changes")
	fmt.Println("────────────  ────────  ─────  ────────  ──────────────────────────────────")
	for _, r := range trail {
		uid := "-"
		if r.UserID != nil {
			uid = *r.UserID
		}
		changesJSON, _ := json.Marshal(r.AuditedChanges)
		fmt.Printf("%-12s  %-8s  v%-4d  %-8s  %s\n", r.AuditableType, r.Action, r.Version, uid, changesJSON)
	}
}
