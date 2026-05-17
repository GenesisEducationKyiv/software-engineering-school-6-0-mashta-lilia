package release

import (
	"database/sql"
	"time"
)

type TrackedRepository struct {
	ID            int64          `db:"id"`
	Owner         string         `db:"owner"`
	Name          string         `db:"name"`
	LastSeenTag   sql.NullString `db:"last_seen_tag"`
	LastCheckedAt sql.NullTime   `db:"last_checked_at"`
	CreatedAt     time.Time      `db:"created_at"`
}

func (r *TrackedRepository) FullName() string {
	return r.Owner + "/" + r.Name
}
