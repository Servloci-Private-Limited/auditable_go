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

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	mongoaudit "github.com/vikasavnish/auditable_go/v3/mongodb"
)

// --------------------------------------------------------------------------
// Models
//
// Embed mongoaudit.Model with `bson:",inline"` — you get _id, created_at,
// updated_at, deleted_at and full audit tracking automatically.
//
// Use the same `auditable` struct tags as the GORM package:
//
//   auditable:"only"    whitelist: only tagged fields appear in audit entries
//   auditable:"redact"  value stored as [REDACTED]
//   auditable:"false"   field is never recorded
// --------------------------------------------------------------------------

// Voter — full audit; EpicNumber is redacted.
type Voter struct {
	mongoaudit.Model `bson:",inline"`
	Name             string `bson:"name"`
	EpicNumber       string `bson:"epic_number" auditable:"redact"`
	Age              int    `bson:"age"`
	Status           string `bson:"status"`
}

// Campaign — whitelist: only Name and Status are audited.
type Campaign struct {
	mongoaudit.Model `bson:",inline"`
	Name             string `bson:"name"       auditable:"only"`
	Status           string `bson:"status"     auditable:"only"`
	ProgramID        int    `bson:"program_id"` // not audited — no "only" tag
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

	voters := auditor.Collection("voters")
	campaigns := auditor.Collection("campaigns")

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

	// ── Audit trail ───────────────────────────────────────────────────────

	cursor, err := auditor.AuditCollection().Find(ctx, bson.M{}, options.Find().SetSort(bson.M{"_id": 1}))
	if err != nil {
		log.Fatal(err)
	}
	var trail []mongoaudit.Audit
	if err := cursor.All(ctx, &trail); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\n%-10s  %-8s  %-5s  %-8s\n", "type", "action", "ver", "user")
	fmt.Println("──────────  ────────  ─────  ────────")
	for _, r := range trail {
		uid := "-"
		if r.UserID != nil {
			uid = *r.UserID
		}
		fmt.Printf("%-10s  %-8s  v%-4d  %s\n", r.AuditableType, r.Action, r.Version, uid)
	}
}
