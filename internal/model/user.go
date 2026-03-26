package model

import "time"

const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

// User is an authenticated account that can access the web UI.
type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"` // RoleAdmin or RoleViewer
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
