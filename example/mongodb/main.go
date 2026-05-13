package main

// MongoDB audit example
//
// Start MongoDB first:
//
//	docker compose up -d        (from this directory)
//
// Then run:
//
//	go run .                    (from this directory)

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	mongoaudit "github.com/vikasavnish/auditable_go/v3/mongodb"
)

// Ensure time is used (batch soft-delete inline)
var _ = time.Now

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
	Name             string `bson:"name"       auditable:"true"`
	Status           string `bson:"status"     auditable:"true"`
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

	ctx := context.Background()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatal("connect:", err)
	}
	defer client.Disconnect(ctx)

	// Register the auditor once for the database.
	// UserIDResolver is the only required config — point it at your context key.
	auditor := mongoaudit.NewAuditor(client.Database("auditdemo"), "audits", mongoaudit.Config{
		UserIDResolver: func(ctx context.Context) (string, bool) {
			return mongoaudit.ExtractUserID(ctx, userKey)
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

	// Every operation below uses a plain context.WithValue context.
	// The auditor reads the user automatically — no WithUserID calls needed.
	// Timestamps (CreatedAt / UpdatedAt) are set automatically on insert.
	ctx = withUser(ctx, "user-1")

	// ── Voter ─────────────────────────────────────────────────────────────

	v := &Voter{Name: "Ravi Kumar", EpicNumber: "EPIC001", Age: 42, Status: "active"}
	res, err := voters.InsertOne(ctx, v) // EpicNumber stored as [REDACTED]
	if err != nil {
		log.Fatal(err)
	}
	voterID := res.InsertedID.(bson.ObjectID)

	voters.UpdateOne(ctx,
		bson.M{"_id": voterID},
		bson.M{"$set": bson.M{"age": 43}},
	)

	// delete audited only for create/update (gated by auditable:"create,update")
	voters.DeleteOne(ctx, bson.M{"_id": voterID})

	// ── Campaign ──────────────────────────────────────────────────────────

	c := &Campaign{Name: "Door-to-Door Drive", Status: "draft", ProgramID: 10}
	res, err = campaigns.InsertOne(ctx, c) // only Name+Status audited; ProgramID skipped
	if err != nil {
		log.Fatal(err)
	}
	campaignID := res.InsertedID.(bson.ObjectID)

	campaigns.UpdateOne(ctx,
		bson.M{"_id": campaignID},
		bson.M{"$set": bson.M{"status": "active"}},
	)

	// ── Category: soft delete + query + restore ───────────────────────────
	//
	// MongoDB has no built-in soft-delete; set deleted_at manually.
	// Normal queries must filter { deleted_at: { $exists: false } }.
	// The auditor captures the $set as an "update" entry.

	cat := &Category{Name: "Technology", Slug: "technology", DisplayOrder: 1}
	res, err = categories.InsertOne(ctx, cat)
	if err != nil {
		log.Fatal(err)
	}
	catID := res.InsertedID.(bson.ObjectID)

	categories.UpdateOne(ctx,
		bson.M{"_id": catID},
		bson.M{"$set": bson.M{"name": "Tech", "slug": "tech"}},
	)

	// Soft-delete: sets deleted_at, audited as "update".
	categories.SoftDelete(ctx, bson.M{"_id": catID})

	// Query — NotDeleted wraps any filter with { deleted_at: { $exists: false } }.
	visible, _ := categories.CountDocuments(ctx, mongoaudit.NotDeleted(bson.M{}))
	fmt.Printf("\nVisible categories after soft-delete: %d\n", visible) // 0

	allCats, _ := categories.CountDocuments(ctx, bson.M{})
	fmt.Printf("All categories (no filter): %d\n", allCats) // 1

	// Restore: unsets deleted_at, audited as "update".
	categories.Restore(ctx, bson.M{"_id": catID})
	visible, _ = categories.CountDocuments(ctx, mongoaudit.NotDeleted(bson.M{}))
	fmt.Printf("Visible categories after restore: %d\n", visible) // 1

	// ── Product: redaction + soft delete ─────────────────────────────────

	prod := &Product{
		Name:        "Widget Pro",
		Price:       49.99,
		CategoryID:  catID.Hex(),
		APIKey:      "sk-live-secret123",
		InternalSKU: "WDG-001",
	}
	res, err = products.InsertOne(ctx, prod) // APIKey → [REDACTED], InternalSKU absent
	if err != nil {
		log.Fatal(err)
	}
	prodID := res.InsertedID.(bson.ObjectID)

	products.UpdateOne(ctx,
		bson.M{"_id": prodID},
		bson.M{"$set": bson.M{"price": 39.99, "name": "Widget Pro (Sale)"}},
	)

	// Soft-delete; [REDACTED] remains in audit log, InternalSKU never appears.
	products.SoftDelete(ctx, bson.M{"_id": prodID})

	// ── Tag: batch soft-delete with UpdateMany ────────────────────────────
	//
	// Create several tags for an article, then soft-delete them all at once.
	// UpdateMany emits one audit entry per affected document.

	articleID := bson.NewObjectID()

	tagDocs := []interface{}{
		&Tag{Name: "go", ArticleID: articleID.Hex()},
		&Tag{Name: "audit", ArticleID: articleID.Hex()},
		&Tag{Name: "mongodb", ArticleID: articleID.Hex()},
	}
	tags.InsertMany(ctx, tagDocs)

	// Batch soft-delete all tags for the article.
	tags.SoftDeleteMany(ctx, bson.M{"article_id": articleID.Hex()})

	remaining, _ := tags.CountDocuments(ctx, mongoaudit.NotDeleted(bson.M{"article_id": articleID.Hex()}))
	fmt.Printf("\nVisible tags after batch soft-delete: %d\n", remaining) // 0

	// Hard-delete one tag permanently (no soft-delete protection).
	// Uses DeleteOne which audits the removal.
	tags.DeleteOne(ctx, bson.M{"article_id": articleID.Hex()})

	// ── Audit trail ───────────────────────────────────────────────────────

	cursor, err := auditor.AuditCollection().Find(ctx, bson.M{}, options.Find().SetSort(bson.M{"_id": 1}))
	if err != nil {
		log.Fatal(err)
	}
	var trail []mongoaudit.Audit
	if err := cursor.All(ctx, &trail); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\n%-12s  %-8s  %-5s  %-8s\n", "type", "action", "ver", "user")
	fmt.Println("────────────  ────────  ─────  ────────")
	for _, r := range trail {
		uid := "-"
		if r.UserID != nil {
			uid = *r.UserID
		}
		fmt.Printf("%-12s  %-8s  v%-4d  %s\n", r.AuditableType, r.Action, r.Version, uid)
	}
}
