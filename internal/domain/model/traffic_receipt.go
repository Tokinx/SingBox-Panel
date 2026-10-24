package model

// TrafficReportCursor is committed atomically with a batch's usage. Keeping
// one high-water mark per Agent process session makes reconnect resends safe
// even if the panel restarted after commit but before sending its receipt.
type TrafficReportCursor struct {
	ServerID uint   `gorm:"primaryKey;autoIncrement:false"`
	Session  string `gorm:"primaryKey;size:36"`
	Sequence uint64 `gorm:"not null"`
}
