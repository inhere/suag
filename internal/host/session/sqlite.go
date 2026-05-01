package session

import (
	"database/sql"

	"github.com/inhere/axon/internal/data"
)

type SQLiteRepository struct {
	db *sql.DB
}

func NewSQLiteRepository(dbPath string) (*SQLiteRepository, error) {
	db, err := data.OpenSQLite(dbPath)
	if err != nil {
		return nil, err
	}

	// Schema ownership is delegated to miglite SQL migrations under db/migrations.
	// Repository startup only verifies the expected tables are already present.
	if err := ensureRequiredTables(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &SQLiteRepository{db: db}, nil
}
