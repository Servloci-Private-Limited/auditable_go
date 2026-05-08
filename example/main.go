package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"

	"github.com/joho/godotenv"
	"github.com/vikasavnish/auditable_go/v3/models"
	auditable "github.com/vikasavnish/auditable_go/v3/postgres-gorm"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const userID = "101"

func must(label string, res *gorm.DB) {
	if res.Error != nil {
		fmt.Printf("  ✗ ERROR [%s]: %v\n", label, res.Error)
	} else {
		fmt.Printf("  ✓ %s  (rows affected: %d)\n", label, res.RowsAffected)
	}
}

func rowCount(db *gorm.DB, table string) int64 {
	var count int64
	db.Table(table).Count(&count)
	return count
}

func main() {
	_ = godotenv.Load(".env")
	dsn := os.Getenv("PRIMARY_DSN_2")

	fmt.Println("Connecting to DB...")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		panic("failed to connect database: " + err.Error())
	}

	fmt.Println("Registering auditable plugin...")
	if err := db.Use(auditable.New(auditable.Config{})); err != nil {
		panic("failed to register plugin: " + err.Error())
	}

	// Stream every new audit row automatically via GORM callback.
	db.Callback().Create().After("auditablegorm:create").Register("stream:audit", func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Table != "audits" {
			return
		}
		rv := tx.Statement.ReflectValue
		if rv.Kind() == reflect.Ptr {
			rv = rv.Elem()
		}
		if rv.Kind() != reflect.Struct {
			return
		}
		a, ok := rv.Interface().(auditable.Audit)
		if !ok {
			return
		}
		uid := "-"
		if a.UserID != nil {
			uid = *a.UserID
		}
		changesJSON, _ := json.MarshalIndent(map[string]interface{}(a.AuditedChanges), "    ", "  ")
		fmt.Printf("  → [audit #%d] %s:%s  action=%-8s user=%s  v%d\n    %s\n",
			a.ID, a.AuditableType, a.AuditableID, a.Action, uid, a.Version, changesJSON)
	})

	fmt.Println("Creating enum types...")
	db.Exec(`DO $$ BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'campaign_status') THEN
			CREATE TYPE campaign_status AS ENUM ('draft', 'active', 'paused', 'completed', 'published');
		END IF;
	END $$`)
	db.Exec(`DROP TYPE IF EXISTS "ACTIONS"`)
	db.Exec(`DO $$ BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'actions') THEN
			CREATE TYPE actions AS ENUM ('mark_as_voted', 'print_voter_slip', 'send_to_whatsapp');
		END IF;
	END $$`)
	db.Exec(`DO $$ BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'program_status') THEN
			CREATE TYPE program_status AS ENUM ('upcoming', 'live', 'completed');
		END IF;
	END $$`)

	fmt.Println("Migrating schemas...")
	if err := db.AutoMigrate(
		&auditable.Audit{},
		&models.Campaign{},
		&models.CampaignAction{},
		&models.CampaignForm{},
		&models.Form{},
		&models.FormSubmission{},
		&models.FormSubmissionMultiple{},
		&models.Program{},
		&models.Location{},
		&models.Vidhansabha{},
		&models.Booth{},
		&models.AdministrativeDistrict{},
		&models.Block{},
		&models.LocalBody{},
		&models.Ward{},
		&models.LocalBodyBooth{},
		&models.VoterAction{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	ctx := auditable.WithUserID(context.Background(), userID)
	d := db.WithContext(ctx)

	ptr := func(v int) *int { return &v }
	str := func(v string) *string { return &v }

	fmt.Printf("\n═══════════════════════════════════════\n")
	fmt.Printf("  Seed ops — UserID %s\n", userID)
	fmt.Printf("═══════════════════════════════════════\n")

	// ══════════════════════════════════════════
	// CAMPAIGNS  (table: program_campaigns)
	// ══════════════════════════════════════════
	fmt.Println("\n── Campaigns ─────────────────────────")

	campaigns := []models.Campaign{
		{
			ProgramID:    10,
			Name:         "Door-to-Door Drive",
			Status:       "draft",
			CampaignType: str("door-to-door"),
			Description:  str("Initial voter contact across Ward 5"),
			CreatedByID:  ptr(101),
		},
		{
			ProgramID:    10,
			Name:         "WhatsApp Blitz",
			Status:       "draft",
			CampaignType: str("digital"),
			Description:  str("Bulk WhatsApp outreach for upcoming election"),
			CreatedByID:  ptr(101),
		},
		{
			ProgramID:    11,
			Name:         "Booth Agent Training",
			Status:       "draft",
			CampaignType: str("training"),
			Description:  str("Training booth-level agents before polling day"),
			CreatedByID:  ptr(102),
		},
	}

	for i := range campaigns {
		must(fmt.Sprintf("campaign[%d].create", i+1), d.Create(&campaigns[i]))
		fmt.Printf("    id=%d  name=%q\n", campaigns[i].ID, campaigns[i].Name)
	}

	// Promote first two campaigns to active
	fmt.Println("\n  Updates — promote to active:")
	must("campaign[1].update status→active", d.Model(&campaigns[0]).Updates(map[string]interface{}{
		"status": "active",
		"name":   "Door-to-Door Drive — Phase 1",
	}))
	must("campaign[2].update status→active", d.Model(&campaigns[1]).Updates(map[string]interface{}{
		"status":       "active",
		"banner_image": "https://cdn.example.com/banners/whatsapp-blitz.png",
	}))

	// Pause third campaign
	fmt.Println("\n  Updates — pause campaign[3]:")
	must("campaign[3].update status→paused", d.Model(&campaigns[2]).Updates(map[string]interface{}{
		"status": "paused",
	}))

	fmt.Printf("\n  Live rows in program_campaigns: %d\n", rowCount(d, "program_campaigns"))

	// Soft-delete the third campaign (sets deleted_at, audit action = delete).
	fmt.Println("\n  Deletes — soft-delete campaign[3]:")
	must("campaign[3].delete", d.Delete(&campaigns[2]))
	fmt.Printf("  Live rows after delete: %d  (soft-deleted row still in DB)\n", rowCount(d, "program_campaigns"))

	// ══════════════════════════════════════════
	// CAMPAIGN ACTIONS  (table: campaign_actions)
	// ══════════════════════════════════════════
	fmt.Println("\n── Campaign Actions ──────────────────")

	actions := []models.CampaignAction{
		{
			ProgramID:         10,
			ProgramCampaignID: uint64(campaigns[0].ID),
			GroupName:         "voter-contact",
			ActionName:        "Mark as Visited",
			ActionText:        "Mark voter household as visited by agent",
			Status:            "active",
			Priority:          1,
			ExecutionOrder:    1,
			CreatedByID:       101,
		},
		{
			ProgramID:         10,
			ProgramCampaignID: uint64(campaigns[0].ID),
			GroupName:         "voter-contact",
			ActionName:        "Send WhatsApp",
			ActionText:        "Send templated WhatsApp message to voter",
			Status:            "active",
			Priority:          2,
			ExecutionOrder:    2,
			CreatedByID:       101,
		},
		{
			ProgramID:         10,
			ProgramCampaignID: uint64(campaigns[1].ID),
			GroupName:         "digital-outreach",
			ActionName:        "Record Phone Response",
			ActionText:        "Log voter response from phone call",
			Status:            "active",
			Priority:          1,
			ExecutionOrder:    1,
			CreatedByID:       101,
		},
	}

	for i := range actions {
		must(fmt.Sprintf("action[%d].create", i+1), d.Create(&actions[i]))
		fmt.Printf("    id=%d  name=%q\n", actions[i].ID, actions[i].ActionName)
	}

	fmt.Println("\n  Updates — reprioritise:")
	must("action[2].update priority→1", d.Model(&actions[1]).Updates(map[string]interface{}{
		"priority": 1,
	}))
	must("action[1].update priority→2", d.Model(&actions[0]).Updates(map[string]interface{}{
		"priority": 2,
	}))

	fmt.Printf("\n  Live rows in campaign_actions: %d\n", rowCount(d, "campaign_actions"))

	// ══════════════════════════════════════════
	// FORMS  (table: forms)
	// ══════════════════════════════════════════
	fmt.Println("\n── Forms ─────────────────────────────")

	forms := []models.Form{
		{Name: "Voter Contact Form", Description: "Capture basic voter contact info"},
		{Name: "Household Survey", Description: "Record household-level demographic data"},
		{Name: "Booth Feedback Form", Description: "Collect post-visit booth agent feedback"},
	}

	for i := range forms {
		must(fmt.Sprintf("form[%d].create", i+1), d.Create(&forms[i]))
		fmt.Printf("    id=%d  name=%q\n", forms[i].ID, forms[i].Name)
	}

	fmt.Println("\n  Updates — version bump:")
	must("form[1].update name→v2", d.Model(&forms[0]).Update("Name", "Voter Contact Form v2"))
	must("form[2].update desc", d.Model(&forms[1]).Update("Description", "Revised: household + GPS data"))

	fmt.Printf("\n  Rows in forms: %d\n", rowCount(db, "forms"))

	// ══════════════════════════════════════════
	// EXAMPLE: audit comment on context
	// Attach a human-readable reason to the audit row via WithComment.
	// ══════════════════════════════════════════
	fmt.Println("\n── Audit with comment ────────────────")
	ctxWithComment := auditable.WithComment(
		auditable.WithUserID(context.Background(), "202"),
		"bulk-status-correction by admin",
	)
	must("form[3].update with comment", db.WithContext(ctxWithComment).
		Model(&forms[2]).
		Update("Description", "Booth feedback form — corrected description"))

	// ══════════════════════════════════════════
	// EXAMPLE: batch create (slice) — one audit row per record
	// ══════════════════════════════════════════
	fmt.Println("\n── Batch create (slice) ──────────────")
	extraForms := []models.Form{
		{Name: "Registration Form", Description: "New voter registration intake"},
		{Name: "Grievance Form", Description: "Voter grievance submission"},
	}
	must("extra forms batch create", d.Create(&extraForms))
	for i := range extraForms {
		fmt.Printf("    id=%d  name=%q\n", extraForms[i].ID, extraForms[i].Name)
	}

	// ══════════════════════════════════════════
	// EXAMPLE: hard delete (Unscoped) — physically removes the row
	// auditable still fires and records action=delete in the audit trail.
	// ══════════════════════════════════════════
	fmt.Println("\n── Hard delete (Unscoped) ────────────")
	must("extra forms[0].hard delete", d.Unscoped().Delete(&extraForms[0]))
	fmt.Printf("  Rows in forms after hard delete: %d\n", rowCount(db, "forms"))

	// ══════════════════════════════════════════
	// FINAL TABLE COUNTS
	// ══════════════════════════════════════════
	fmt.Println("\n── Table counts ──────────────────────")
	for _, t := range []string{"program_campaigns", "campaign_actions", "forms", "audits"} {
		fmt.Printf("  %-28s %d rows\n", t, rowCount(db, t))
	}

	// ══════════════════════════════════════════
	// AUDIT TRAIL FOR THIS RUN
	// ══════════════════════════════════════════
	fmt.Println("\n── Audit trail (this run) ────────────")
	type auditRow struct {
		ID            uint64
		AuditableType string
		AuditableID   string
		Action        string
		Version       uint64
	}
	var trail []auditRow
	// show last 20 audit entries ordered by id desc
	db.Table("audits").
		Select("id, auditable_type, auditable_id, action, version").
		Order("id desc").
		Limit(20).
		Scan(&trail)
	fmt.Printf("  %-6s %-20s %-6s %-10s %s\n", "audit", "type", "id", "action", "ver")
	fmt.Println("  " + "───────────────────────────────────────────────────────")
	for _, r := range trail {
		fmt.Printf("  %-6d %-20s %-6s %-10s v%d\n", r.ID, r.AuditableType, r.AuditableID, r.Action, r.Version)
	}

	fmt.Println("\nDone.")
}
