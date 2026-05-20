package models

type NotificationSetting struct {
	ID               uint             `gorm:"primaryKey;autoIncrement"`
	AgentID          uint             `gorm:"not null;uniqueIndex:idx_notif_settings_agent_type"`
	NotificationType string           `gorm:"type:varchar(50);not null;uniqueIndex:idx_notif_settings_agent_type"`
	Enabled          bool             `gorm:"not null;default:true"`
	Channel          string           `gorm:"type:varchar(20);not null;default:in_app"` // in_app, email, webhook

	Agent Agent `gorm:"foreignKey:AgentID;constraint:OnDelete:CASCADE"`
}
