package repository

import (
	"database/sql"
	"time"
)

type Repository struct {
	ID            int64          `db:"id"`
	Owner         string         `db:"owner"`
	Name          string         `db:"name"`
	LastSeenTag   sql.NullString `db:"last_seen_tag"`
	LastCheckedAt sql.NullTime   `db:"last_checked_at"`
	CreatedAt     time.Time      `db:"created_at"`
}

func (r *Repository) FullName() string {
	return r.Owner + "/" + r.Name
}
