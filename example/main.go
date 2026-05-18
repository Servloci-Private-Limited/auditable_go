package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	auditable "github.com/vikasavnish/auditable_go/v5/postgres-gorm"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// --------------------------------------------------------------------------
// Models
//
// 1. Embed auditable.Model — you get ID, CreatedAt, UpdatedAt, DeletedAt
//    and full audit tracking automatically.
//
// 2. Control per-field audit behaviour with the `auditable` struct tag:
//
//    auditable:"true"    whitelist: only tagged fields appear in audit entries
//    auditable:"redact"  value stored as [REDACTED] (e.g. passwords, tokens)
//    auditable:"-"       field is never recorded
//    (no tag)            always recorded, unless another field has "true"
// --------------------------------------------------------------------------

// Article — whitelist: only Title and Status are audited.
// ViewCount changes are silently ignored.
type Article struct {
	auditable.Model
	Title     string `gorm:"not null"        auditable:"true"`
	Status    string `gorm:"default:'draft'" auditable:"true"`
	ViewCount int
}

// User — full audit; Password is redacted, InternalNote is never recorded.
type User struct {
	auditable.Model
	Name         string `gorm:"not null"`
	Email        string `gorm:"uniqueIndex;not null"`
	Password     string `gorm:"not null" auditable:"redact"`
	InternalNote string `gorm:"size:500"  auditable:"-"`
}

// Comment — full audit, no special rules.
type Comment struct {
	auditable.Model
	Body      string `gorm:"type:text;not null"`
	ArticleID uint   `gorm:"not null"`
}

// Category — whitelist: only Name and Slug are audited.
// DisplayOrder changes are silently ignored.
type Category struct {
	auditable.Model
	Name         string `gorm:"not null"          auditable:"true"`
	Slug         string `gorm:"uniqueIndex;not null" auditable:"true"`
	DisplayOrder int
}

// Product — full audit; APIKey is redacted, InternalSKU is never recorded.
type Product struct {
	auditable.Model
	Name        string  `gorm:"not null"`
	Price       float64 `gorm:"not null"`
	CategoryID  uint    `gorm:"not null"`
	APIKey      string  `gorm:"size:128" auditable:"redact"`
	InternalSKU string  `gorm:"size:64"  auditable:"-"`
}

// Tag — full audit, no special rules. Demonstrates batch soft-delete.
type Tag struct {
	auditable.Model
	Name      string `gorm:"uniqueIndex;not null"`
	ArticleID uint   `gorm:"not null"`
}

// --------------------------------------------------------------------------
// Context key — set once by your auth middleware, read by the plugin.
// Accepts any type: string, int, uint, int64 …
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
	_ = godotenv.Load(".env")

	db, err := gorm.Open(postgres.Open(os.Getenv("PRIMARY_DSN_2")), &gorm.Config{})
	if err != nil {
		log.Fatal(err)
	}

	// Register the plugin once at startup.
	// UserIDResolver is the only required config — point it at your context key.
	if err := db.Use(auditable.New(auditable.Config{
		UserIDResolver: func(ctx context.Context) (string, bool) {
			return auditable.ExtractUserID(ctx, userKey)
		},
	})); err != nil {
		log.Fatal(err)
	}

	// Migrate — creates the audits table + your own tables.
	auditable.Migrate(db)
	db.AutoMigrate(&Article{}, &User{}, &Comment{}, &Category{}, &Product{}, &Tag{})

	// Every operation below uses db.WithContext(ctx).
	// The plugin reads the user automatically — no extra calls needed.
	ctx := withUser(context.Background(), 1)
	d := db.WithContext(ctx)

	// ── create / update / delete ──────────────────────────────────────────

	article := &Article{Title: "Hello World", Status: "draft"}
	d.Create(article)

	d.Model(article).Updates(map[string]any{"title": "Hello World (revised)", "status": "published"})
	d.Model(article).Update("ViewCount", 42) // not audited — no "only" tag

	d.Delete(article) // soft-delete; audit action = "delete"

	user := &User{Name: "Alice", Email: "alice@example.com", Password: "s3cr3t"}
	d.Create(user) // Password stored as [REDACTED] in the audit trail

	d.Model(user).Update("Email", "alice@new.example.com")

	comment := &Comment{Body: "Great article!", ArticleID: article.ID}
	d.Create(comment)

	d.Model(comment).Update("Body", "Great article! (edited)")

	// ── Category: soft delete + query deleted + restore ───────────────────
	//
	// gorm.Model embeds DeletedAt (gorm.DeletedAt). d.Delete() sets it;
	// normal queries exclude it; Unscoped() bypasses the filter.

	cat := &Category{Name: "Technology", Slug: "technology", DisplayOrder: 1}
	d.Create(cat)
	d.Model(cat).Updates(map[string]any{"name": "Tech", "slug": "tech"})

	// Soft-delete the category.
	d.Delete(cat) // sets deleted_at, audit action = "delete"

	// Query — soft-deleted rows are invisible by default.
	var visible []Category
	d.Find(&visible)
	fmt.Printf("\nVisible categories after soft-delete: %d\n", len(visible)) // 0

	// Query including soft-deleted rows.
	var all []Category
	d.Unscoped().Find(&all)
	fmt.Printf("All categories (Unscoped): %d\n", len(all)) // 1

	// Restore: clear DeletedAt.
	d.Unscoped().Model(cat).Update("DeletedAt", nil)
	d.Find(&visible)
	fmt.Printf("Visible categories after restore: %d\n", len(visible)) // 1

	// ── Product: redaction + soft delete ─────────────────────────────────

	prod := &Product{
		Name:        "Widget Pro",
		Price:       49.99,
		CategoryID:  cat.ID,
		APIKey:      "sk-live-secret123",
		InternalSKU: "WDG-001",
	}
	d.Create(prod) // APIKey → [REDACTED], InternalSKU never recorded

	d.Model(prod).Updates(map[string]any{"price": 39.99, "name": "Widget Pro (Sale)"})
	d.Delete(prod) // soft-delete; [REDACTED] kept in audit log, InternalSKU absent

	// ── Tag: batch soft-delete ────────────────────────────────────────────
	//
	// Create several tags for the article, then delete them all at once.
	// Each individual row gets its own audit entry (one per affected ID).

	article2 := &Article{Title: "Batch Demo", Status: "draft"}
	d.Create(article2)

	tags := []Tag{
		{Name: "go", ArticleID: article2.ID},
		{Name: "audit", ArticleID: article2.ID},
		{Name: "gorm", ArticleID: article2.ID},
	}
	d.Create(&tags)

	// Batch soft-delete all tags belonging to article2.
	d.Where("article_id = ?", article2.ID).Delete(&Tag{})

	// Confirm: no visible tags remain for that article.
	var remainingTags []Tag
	d.Where("article_id = ?", article2.ID).Find(&remainingTags)
	fmt.Printf("\nVisible tags after batch soft-delete: %d\n", len(remainingTags)) // 0

	// Hard-delete one specific tag permanently (no soft-delete protection).
	d.Unscoped().Delete(&tags[0])

	// ── Soft delete inside a transaction ──────────────────────────────────
	//
	// Deletes and their audit rows are committed or rolled back atomically.

	article3 := &Article{Title: "To Be Purged", Status: "draft"}
	d.Create(article3)
	comment2 := &Comment{Body: "Spam comment", ArticleID: article3.ID}
	d.Create(comment2)

	err = d.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(comment2).Error; err != nil { // soft-delete comment
			return err
		}
		if err := tx.Delete(article3).Error; err != nil { // soft-delete article
			return err
		}
		return nil // both soft-deletes + audit rows committed atomically
	})
	if err != nil {
		log.Println("transaction failed:", err)
	}

	// ── transaction ───────────────────────────────────────────────────────
	//
	// Audit rows are written inside the same transaction as the data.
	// A rollback undoes both — no orphan audit rows.

	err = d.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&Article{Title: "Atomic Post", Status: "draft"}).Error; err != nil {
			return err
		}
		if err := tx.Create(&User{Name: "Bob", Email: "bob@example.com", Password: "hunter2"}).Error; err != nil {
			return err
		}
		return nil // commit: all data + all audit rows land atomically
	})
	if err != nil {
		log.Println("transaction failed:", err)
	}

	// ── audit trail ───────────────────────────────────────────────────────

	var trail []struct {
		ID            uint64
		AuditableType string
		AuditableID   string
		Action        string
		Version       uint64
		UserID        *string
		Comment       *string
	}
	db.Table("audits").Order("id").Scan(&trail)

	fmt.Printf("\n%-4s  %-10s  %-8s  %-5s  %-8s  %s\n", "id", "type", "action", "ver", "user", "comment")
	fmt.Println("────  ──────────  ────────  ─────  ────────  ───────────────────────")
	for _, r := range trail {
		uid, cmt := "-", "-"
		if r.UserID != nil {
			uid = *r.UserID
		}
		if r.Comment != nil {
			cmt = *r.Comment
		}
		fmt.Printf("%-4d  %-10s  %-8s  v%-4d  %-8s  %s\n",
			r.ID, r.AuditableType, r.Action, r.Version, uid, cmt)
	}
}
