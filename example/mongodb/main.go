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
//
// Set MONGO_URI to override the default connection string.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	mongoaudit "github.com/vikasavnish/auditable_go/v3/mongodb"
)

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

	if err := client.Ping(ctx, nil); err != nil {
		log.Fatal("ping:", err)
	}
	fmt.Println("Connected to MongoDB")

	db := client.Database("auditdemo")
	auditsColl := db.Collection("audits")

	// Drop all collections for a clean demo run.
	for _, name := range []string{"voters", "campaigns", "campaign_actions", "voter_actions", "audits"} {
		_ = db.Collection(name).Drop(ctx)
	}

	cfg := mongoaudit.Config{
		RedactedFields: map[string]struct{}{"epic_number": {}},
		OnAudit:        printAudit,
		OnError: func(err error) {
			log.Printf("audit error: %v", err)
		},
	}

	auditor := mongoaudit.NewAuditor(db, auditsColl.Name(), cfg)
	if err := auditor.EnsureIndexes(ctx); err != nil {
		log.Fatal("audit indexes:", err)
	}

	voters := auditor.Collection("voters")
	campaigns := auditor.Collection("campaigns")
	campaignActions := auditor.Collection("campaign_actions")
	voterActions := auditor.Collection("voter_actions")

	// Context carries the acting user for all operations in this session.
	actorCtx := mongoaudit.WithUserID(ctx, "user-101")

	// ════════════════════════════════════════════════════════════
	// VOTERS  (matches VoterBase / VoterAtlas shape)
	// ════════════════════════════════════════════════════════════
	fmt.Println("\n══ voters ══════════════════════════════════════════════")

	_, err = voters.InsertMany(actorCtx, []interface{}{
		bson.M{
			"_id": "EPIC001", "name": "Ravi Kumar", "relation_name": "Suresh Kumar",
			"relation_type": "Father", "house_number": "12A", "age": 42,
			"gender": "M", "page_number": 5, "epic_number": "EPIC001",
			"state_id": 9, "state_name": "Uttar Pradesh",
			"ac_id": 101, "ac_name": "Lucknow East", "ac_number": int64(101),
			"booth_id": int64(20), "booth_name": "Booth 20", "booth_number": int64(20),
			"serial_number": int64(45),
			"tags":          bson.A{},
			"created_at":    time.Now(), "updated_at": time.Now(),
		},
		bson.M{
			"_id": "EPIC002", "name": "Priya Sharma", "relation_name": "Mohan Sharma",
			"relation_type": "Father", "house_number": "7B", "age": 35,
			"gender": "F", "page_number": 3, "epic_number": "EPIC002",
			"state_id": 9, "state_name": "Uttar Pradesh",
			"ac_id": 101, "ac_name": "Lucknow East", "ac_number": int64(101),
			"booth_id": int64(20), "booth_name": "Booth 20", "booth_number": int64(20),
			"serial_number": int64(46),
			"tags":          bson.A{},
			"created_at":    time.Now(), "updated_at": time.Now(),
		},
	})
	if err != nil {
		log.Fatal("voters InsertMany:", err)
	}
	fmt.Println("  inserted 2 voters")

	// Voter tag update
	tagCtx := mongoaudit.WithComment(actorCtx, "field team tagged voter")
	_, err = voters.UpdateOne(tagCtx,
		bson.M{"_id": "EPIC001"},
		bson.M{"$set": bson.M{
			"tags":       bson.A{bson.M{"key": "supporter", "color": "#4CAF50"}},
			"updated_at": time.Now(),
		}},
	)
	if err != nil {
		log.Fatal("voters UpdateOne:", err)
	}

	// Bulk mark both voters as actioned for campaign c1
	_, err = voters.UpdateMany(actorCtx,
		bson.M{"ac_id": 101},
		bson.M{"$addToSet": bson.M{"actioned_campaigns": "c1"}},
	)
	if err != nil {
		log.Fatal("voters UpdateMany:", err)
	}

	// Soft-delete second voter
	delCtx := mongoaudit.WithComment(actorCtx, "duplicate record removed")
	_, err = voters.DeleteOne(delCtx, bson.M{"_id": "EPIC002"})
	if err != nil {
		log.Fatal("voters DeleteOne:", err)
	}

	// ════════════════════════════════════════════════════════════
	// CAMPAIGNS  (matches Campaign shape)
	// ════════════════════════════════════════════════════════════
	fmt.Println("\n══ campaigns ═══════════════════════════════════════════")

	_, err = campaigns.InsertOne(actorCtx, bson.M{
		"_id": "c1", "program_id": 10, "name": "Door-to-Door Drive",
		"status": "draft", "campaign_type": "door-to-door",
		"description":   "Initial voter contact across Ward 5",
		"created_by_id": 101,
		"created_at":    time.Now(), "updated_at": time.Now(),
	})
	if err != nil {
		log.Fatal("campaigns InsertOne:", err)
	}

	publishCtx := mongoaudit.WithComment(actorCtx, "approved by programme manager")
	_, err = campaigns.UpdateOne(publishCtx,
		bson.M{"_id": "c1"},
		bson.M{"$set": bson.M{"status": "active", "updated_at": time.Now()}},
	)
	if err != nil {
		log.Fatal("campaigns UpdateOne:", err)
	}

	_, err = campaigns.InsertOne(actorCtx, bson.M{
		"_id": "c2", "program_id": 10, "name": "WhatsApp Blitz",
		"status": "draft", "campaign_type": "digital",
		"description":   "Bulk WhatsApp outreach",
		"created_by_id": 101,
		"created_at":    time.Now(), "updated_at": time.Now(),
	})
	if err != nil {
		log.Fatal("campaigns InsertOne c2:", err)
	}
	// Cancel the draft campaign
	_, err = campaigns.DeleteOne(actorCtx, bson.M{"_id": "c2"})
	if err != nil {
		log.Fatal("campaigns DeleteOne:", err)
	}

	// ════════════════════════════════════════════════════════════
	// CAMPAIGN ACTIONS  (matches CampaignAction shape)
	// ════════════════════════════════════════════════════════════
	fmt.Println("\n══ campaign_actions ════════════════════════════════════")

	_, err = campaignActions.InsertMany(actorCtx, []interface{}{
		bson.M{
			"_id": "ca1", "program_id": 10, "program_campaign_id": "c1",
			"group_name": "field-ops", "action_name": "Mark as Voted",
			"action_text": "Confirm voter has cast their vote",
			"status":      "active", "priority": 1, "execution_order": 1,
			"meta":          bson.M{"sticky": "top", "expandable": false},
			"created_by_id": 101,
			"created_at":    time.Now(), "updated_at": time.Now(),
		},
		bson.M{
			"_id": "ca2", "program_id": 10, "program_campaign_id": "c1",
			"group_name": "field-ops", "action_name": "Print Voter Slip",
			"action_text": "Print and hand over the voter slip",
			"status":      "active", "priority": 2, "execution_order": 2,
			"meta":          bson.M{"sticky": "bottom", "expandable": true},
			"created_by_id": 101,
			"created_at":    time.Now(), "updated_at": time.Now(),
		},
	})
	if err != nil {
		log.Fatal("campaign_actions InsertMany:", err)
	}

	// Reprioritise action
	_, err = campaignActions.UpdateOne(actorCtx,
		bson.M{"_id": "ca2"},
		bson.M{"$set": bson.M{"priority": 1, "execution_order": 1, "updated_at": time.Now()}},
	)
	if err != nil {
		log.Fatal("campaign_actions UpdateOne:", err)
	}

	// Deactivate all actions for program 10 in one go
	_, err = campaignActions.UpdateMany(actorCtx,
		bson.M{"program_id": 10},
		bson.M{"$set": bson.M{"status": "inactive", "updated_at": time.Now()}},
	)
	if err != nil {
		log.Fatal("campaign_actions UpdateMany:", err)
	}

	// ════════════════════════════════════════════════════════════
	// VOTER ACTIONS  (matches VoterAction shape)
	// ════════════════════════════════════════════════════════════
	fmt.Println("\n══ voter_actions ═══════════════════════════════════════")

	_, err = voterActions.InsertMany(actorCtx, []interface{}{
		bson.M{
			"_id": "va1", "program_id": 10, "program_campaign_id": "c1",
			"voter_id": "EPIC001", "state_name": "Uttar Pradesh",
			"epic_number": "EPIC001", "country_state_id": int64(9),
			"ac_id": int64(101), "booth_id": int64(20),
			"created_by_id": 101, "action": "mark_as_voted",
			"payload":  bson.M{"confirmed": true},
			"latitude": 26.8467, "longitude": 80.9462,
			"created_at": time.Now(), "updated_at": time.Now(),
		},
		bson.M{
			"_id": "va2", "program_id": 10, "program_campaign_id": "c1",
			"voter_id": "EPIC001", "state_name": "Uttar Pradesh",
			"epic_number": "EPIC001", "country_state_id": int64(9),
			"ac_id": int64(101), "booth_id": int64(20),
			"created_by_id": 101, "action": "print_voter_slip",
			"payload":  bson.M{"copies": 1},
			"latitude": 26.8467, "longitude": 80.9462,
			"created_at": time.Now(), "updated_at": time.Now(),
		},
	})
	if err != nil {
		log.Fatal("voter_actions InsertMany:", err)
	}

	// Correct a wrong location on an action
	_, err = voterActions.UpdateOne(actorCtx,
		bson.M{"_id": "va1"},
		bson.M{"$set": bson.M{
			"latitude":   26.8500,
			"longitude":  80.9500,
			"updated_at": time.Now(),
		}},
	)
	if err != nil {
		log.Fatal("voter_actions UpdateOne:", err)
	}

	// ════════════════════════════════════════════════════════════
	// FULL AUDIT TRAIL
	// ════════════════════════════════════════════════════════════
	fmt.Println("\n═══════════════════════════════════════════════════════════")
	fmt.Println("  Full audit trail (by collection, sorted by created_at)")
	fmt.Println("═══════════════════════════════════════════════════════════")

	for _, colName := range []string{"voters", "campaigns", "campaign_actions", "voter_actions"} {
		cursor, ferr := auditsColl.Find(ctx,
			bson.M{"auditable_type": colName},
			options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}}),
		)
		if ferr != nil {
			log.Fatal(ferr)
		}
		var docs []bson.M
		_ = cursor.All(ctx, &docs)
		cursor.Close(ctx)

		fmt.Printf("\n  ── %s (%d records) ──\n", colName, len(docs))
		for i, doc := range docs {
			b, _ := json.MarshalIndent(doc, "    ", "  ")
			fmt.Printf("  [%d] %s\n", i+1, b)
		}
	}

	// Summary counts per collection.
	fmt.Println("\n  ── summary ──")
	pipeline := mongo.Pipeline{
		{{Key: "$group", Value: bson.M{"_id": "$auditable_type", "count": bson.M{"$sum": 1}}}},
		{{Key: "$sort", Value: bson.M{"_id": 1}}},
	}
	cur, _ := auditsColl.Aggregate(ctx, pipeline)
	var summaries []bson.M
	_ = cur.All(ctx, &summaries)
	cur.Close(ctx)
	for _, s := range summaries {
		fmt.Printf("  %-20s %v records\n", s["_id"], s["count"])
	}
}

func printAudit(a *mongoaudit.Audit) {
	uid := "-"
	if a.UserID != nil {
		uid = *a.UserID
	}
	comment := ""
	if a.Comment != nil {
		comment = "  comment=" + *a.Comment
	}
	b, _ := json.MarshalIndent(map[string]interface{}(a.AuditedChanges), "    ", "  ")
	fmt.Printf("  → [audit v%d] %s/%s  action=%-10s user=%s%s\n    %s\n",
		a.Version, a.AuditableType, a.AuditableID, a.Action, uid, comment, b)
}
