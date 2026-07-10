package subscription

import "time"

type Status string

const (
	StatusPending      Status = "pending"
	StatusActive       Status = "active"
	StatusUnsubscribed Status = "unsubscribed"
)

type Subscription struct {
	ID        int64     `json:"id" db:"id"`
	Email     string    `json:"email" db:"email"`
	RepoOwner string    `json:"repo_owner" db:"repo_owner"`
	RepoName  string    `json:"repo_name" db:"repo_name"`
	Token     string    `json:"-" db:"token"`
	Status    Status    `json:"status" db:"status"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

func (s *Subscription) FullName() string {
	return s.RepoOwner + "/" + s.RepoName
}
