package model

import "time"

// UserTrafficIdentity binds a reported proxy name to the account incarnation
// that owned it on one inbound. Rows deliberately survive account deletion:
// a delayed legacy report must never be rebound to a reused login name.
type UserTrafficIdentity struct {
	ID        uint   `gorm:"primaryKey"`
	ServerID  uint   `gorm:"not null;uniqueIndex:idx_user_traffic_identity,priority:1"`
	InboundID uint   `gorm:"not null;uniqueIndex:idx_user_traffic_identity,priority:2"`
	Name      string `gorm:"size:191;not null;uniqueIndex:idx_user_traffic_identity,priority:3"`
	UserID    uint   `gorm:"not null;index"`
	UserKey   string `gorm:"size:96;not null"`
	CreatedAt time.Time
}
