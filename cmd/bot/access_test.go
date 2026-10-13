package main

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// newTestAccessRepo opens a throwaway in-memory database. MaxOpenConns(1) keeps
// every query on a single connection, otherwise SQLite would treat each pooled
// connection as its own separate ":memory:" database.
func newTestAccessRepo(t *testing.T) *AccessRepository {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open :memory:: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	repo := NewAccessRepository(db)
	if err := repo.EnsureSchema(); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return repo
}

func sampleProfile(id int64) UserProfile {
	return UserProfile{
		TelegramID:   id,
		FirstName:    "Ivan",
		LastName:     "Petrov",
		UserName:     "ivan",
		LanguageCode: "ru",
	}
}

func TestEnsureSchemaIdempotent(t *testing.T) {
	repo := newTestAccessRepo(t)
	if err := repo.EnsureSchema(); err != nil {
		t.Fatalf("second EnsureSchema should be a no-op, got: %v", err)
	}
}

func TestStatusCheckConstraintRejectsUnknown(t *testing.T) {
	repo := newTestAccessRepo(t)
	_, err := repo.db.Exec(`INSERT INTO users (telegram_id, status, created_at) VALUES (1, 'banana', '2026-01-01T00:00:00Z')`)
	if err == nil {
		t.Fatal("CHECK constraint accepted an unknown status")
	}
}

func TestStatusUnknownUserIsNone(t *testing.T) {
	repo := newTestAccessRepo(t)
	status, err := repo.Status(999)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status != StatusNone {
		t.Fatalf("Status = %q, want StatusNone", status)
	}
}

func TestCreatePendingOnceOnly(t *testing.T) {
	repo := newTestAccessRepo(t)

	created, err := repo.CreatePending(sampleProfile(42))
	if err != nil {
		t.Fatalf("first CreatePending: %v", err)
	}
	if !created {
		t.Fatal("first CreatePending = false, want true")
	}

	// A second request from the same user must be a no-op (FR2: one active
	// request per user), must not raise, and must leave the row untouched.
	created, err = repo.CreatePending(UserProfile{TelegramID: 42, FirstName: "Spam"})
	if err != nil {
		t.Fatalf("second CreatePending: %v", err)
	}
	if created {
		t.Fatal("second CreatePending = true, want false")
	}

	var firstName string
	if err := repo.db.QueryRow(`SELECT first_name FROM users WHERE telegram_id = 42`).Scan(&firstName); err != nil {
		t.Fatalf("read back profile: %v", err)
	}
	if firstName != "Ivan" {
		t.Fatalf("profile was overwritten: first_name = %q, want %q", firstName, "Ivan")
	}

	status, err := repo.Status(42)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status != StatusPending {
		t.Fatalf("Status = %q, want pending", status)
	}
}

func TestDecideFromPendingIsApplied(t *testing.T) {
	repo := newTestAccessRepo(t)
	if _, err := repo.CreatePending(sampleProfile(7)); err != nil {
		t.Fatalf("CreatePending: %v", err)
	}

	applied, current, err := repo.Decide(7, StatusApproved)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if !applied {
		t.Fatal("applied = false, want true")
	}
	if current != StatusApproved {
		t.Fatalf("current = %q, want approved", current)
	}

	// decided_at must be filled once the request is decided.
	var decidedAt sql.NullString
	if err := repo.db.QueryRow(`SELECT decided_at FROM users WHERE telegram_id = 7`).Scan(&decidedAt); err != nil {
		t.Fatalf("read decided_at: %v", err)
	}
	if !decidedAt.Valid || decidedAt.String == "" {
		t.Fatalf("decided_at is empty after approval: %+v", decidedAt)
	}
}

func TestDecidePendingRequestHasNoDecidedAt(t *testing.T) {
	repo := newTestAccessRepo(t)
	if _, err := repo.CreatePending(sampleProfile(8)); err != nil {
		t.Fatalf("CreatePending: %v", err)
	}
	var decidedAt sql.NullString
	if err := repo.db.QueryRow(`SELECT decided_at FROM users WHERE telegram_id = 8`).Scan(&decidedAt); err != nil {
		t.Fatalf("read decided_at: %v", err)
	}
	if decidedAt.Valid {
		t.Fatalf("pending request must not have decided_at: %+v", decidedAt)
	}
}

func TestDecideTerminalStatesAreNotReversible(t *testing.T) {
	t.Run("approved then decline", func(t *testing.T) {
		repo := newTestAccessRepo(t)
		if _, err := repo.CreatePending(sampleProfile(1)); err != nil {
			t.Fatalf("CreatePending: %v", err)
		}
		if _, _, err := repo.Decide(1, StatusApproved); err != nil {
			t.Fatalf("first Decide: %v", err)
		}
		applied, current, err := repo.Decide(1, StatusDeclined)
		if err != nil {
			t.Fatalf("second Decide: %v", err)
		}
		if applied {
			t.Fatal("applied = true on an approved user, want false")
		}
		if current != StatusApproved {
			t.Fatalf("current = %q, want approved (unchanged)", current)
		}
	})

	t.Run("declined then approve", func(t *testing.T) {
		repo := newTestAccessRepo(t)
		if _, err := repo.CreatePending(sampleProfile(2)); err != nil {
			t.Fatalf("CreatePending: %v", err)
		}
		if _, _, err := repo.Decide(2, StatusDeclined); err != nil {
			t.Fatalf("first Decide: %v", err)
		}
		applied, current, err := repo.Decide(2, StatusApproved)
		if err != nil {
			t.Fatalf("second Decide: %v", err)
		}
		if applied {
			t.Fatal("applied = true on a declined user, want false")
		}
		if current != StatusDeclined {
			t.Fatalf("current = %q, want declined (unchanged)", current)
		}
	})
}

func TestDecideOnMissingUserIsNoOp(t *testing.T) {
	repo := newTestAccessRepo(t)
	applied, current, err := repo.Decide(12345, StatusApproved)
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if applied {
		t.Fatal("applied = true for a user with no row, want false")
	}
	if current != StatusNone {
		t.Fatalf("current = %q, want StatusNone", current)
	}
}
