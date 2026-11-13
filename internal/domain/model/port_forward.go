package model

import "time"

const (
	ForwardTargetCustom  = "custom"
	ForwardTargetInbound = "inbound"
)

// PortForward represents a managed direct layer-4 port forward on a Server.
type PortForward struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	ServerID        uint      `json:"server_id" gorm:"index;uniqueIndex:idx_server_forward_tag,priority:1;not null"`
	Tag             string    `json:"tag" gorm:"size:128;uniqueIndex:idx_server_forward_tag,priority:2;not null"`
	ListenPort      int       `json:"listen_port" gorm:"not null"`
	Network         string    `json:"network" gorm:"size:16;default:''"`
	TargetType      string    `json:"target_type" gorm:"size:16;not null;default:'custom'"`
	TargetServerID  uint      `json:"target_server_id" gorm:"index;default:0"`
	TargetInboundID uint      `json:"target_inbound_id" gorm:"index;default:0"`
	TargetAddress   string    `json:"target_address" gorm:"size:255;default:''"`
	TargetPort      int       `json:"target_port" gorm:"default:0"`
	Outbound        string    `json:"outbound" gorm:"size:128;not null;default:'direct'"`
	Remark          string    `json:"remark" gorm:"size:255"`
	Enabled         bool      `json:"enabled" gorm:"index"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`

	// Computed display metadata hydrated on admin read endpoints; never persisted.
	TargetServerName string `json:"target_server_name,omitempty" gorm:"-"`
	TargetInboundTag string `json:"target_inbound_tag,omitempty" gorm:"-"`
	ResolvedAddress  string `json:"resolved_address,omitempty" gorm:"-"`
	ResolvedPort     int    `json:"resolved_port,omitempty" gorm:"-"`
}

// TrafficForwardRecord stores per-minute/bucket byte deltas attributed to a port forward.
type TrafficForwardRecord struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	ServerID       uint      `json:"server_id" gorm:"not null;uniqueIndex:idx_tfr_server_forward_bucket,priority:1"`
	ForwardID      uint      `json:"forward_id" gorm:"not null;uniqueIndex:idx_tfr_server_forward_bucket,priority:2"`
	Bucket         time.Time `json:"bucket" gorm:"not null;uniqueIndex:idx_tfr_server_forward_bucket,priority:3;index"`
	Upload         uint64    `json:"upload"`
	Download       uint64    `json:"download"`
	UploadRate     uint64    `json:"upload_rate"`
	DownloadRate   uint64    `json:"download_rate"`
	TCPConnections int       `json:"tcp_connections"`
	UDPConnections int       `json:"udp_connections"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// TrafficForwardHourly holds pre-aggregated UTC hourly traffic deltas per port forward.
type TrafficForwardHourly struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	ServerID       uint      `json:"server_id" gorm:"not null;uniqueIndex:idx_tfh_server_forward_bucket,priority:1"`
	ForwardID      uint      `json:"forward_id" gorm:"not null;uniqueIndex:idx_tfh_server_forward_bucket,priority:2"`
	Bucket         time.Time `json:"bucket" gorm:"not null;uniqueIndex:idx_tfh_server_forward_bucket,priority:3;index"`
	Upload         uint64    `json:"upload"`
	Download       uint64    `json:"download"`
	UploadRate     uint64    `json:"upload_rate"`
	DownloadRate   uint64    `json:"download_rate"`
	TCPConnections int       `json:"tcp_connections"`
	UDPConnections int       `json:"udp_connections"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// TableName keeps the singular rollup naming convention used across traffic tables.
func (TrafficForwardHourly) TableName() string {
	return "traffic_forward_hourly"
}

// ForwardTrafficIdentity permanently associates a port-forward tag with a PortForward ID per server.
type ForwardTrafficIdentity struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	ServerID  uint      `json:"server_id" gorm:"not null;uniqueIndex:idx_fwd_identity_server_tag_key,priority:1"`
	Tag       string    `json:"tag" gorm:"size:128;not null"`
	TagKey    string    `json:"tag_key" gorm:"size:64;not null;uniqueIndex:idx_fwd_identity_server_tag_key,priority:2"`
	ForwardID uint      `json:"forward_id" gorm:"not null;index"`
	CreatedAt time.Time `json:"created_at"`
}
