package database

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	*sql.DB
}

func New(path string) (*DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{DB: db}, nil
}

func (db *DB) Initialize() error {
	schema := `
	CREATE TABLE IF NOT EXISTS organizations (
		id INTEGER PRIMARY KEY,
		title TEXT,
		last_scraped DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		organization_id INTEGER NOT NULL,
		title TEXT NOT NULL,
		description TEXT,
		datetime_start DATETIME NOT NULL,
		datetime_end DATETIME,
		url TEXT NOT NULL UNIQUE,
		location TEXT,
		scraper TEXT DEFAULT 'website',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		campaign TEXT,
		campaign_id INTEGER,
		FOREIGN KEY (organization_id) REFERENCES organizations(id)
	);

	CREATE INDEX IF NOT EXISTS idx_events_org_date ON events(organization_id, datetime_start);
	CREATE INDEX IF NOT EXISTS idx_events_date ON events(datetime_start);
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Migrations for existing databases. Errors are ignored because the
	// columns already exist on fresh databases.
	migrations := []string{
		`ALTER TABLE organizations ADD COLUMN title TEXT`,
		`ALTER TABLE events ADD COLUMN campaign TEXT`,
		`ALTER TABLE events ADD COLUMN campaign_id INTEGER`,
	}
	for _, m := range migrations {
		db.Exec(m)
	}

	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_events_org_campaign_date ON events(organization_id, campaign_id, datetime_start)`); err != nil {
		return fmt.Errorf("failed to create campaign index: %w", err)
	}

	return nil
}
