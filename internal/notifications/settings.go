package notifications

import (
	"fmt"

	"chick/internal/models"
)

// AllNotificationTypes returns all supported notification types with descriptions.
func AllNotificationTypes() []map[string]string {
	return []map[string]string{
		{"type": string(NotifIssueAssigned), "description": "Issue assigned to you"},
		{"type": string(NotifCommentMention), "description": "New comment on your issue"},
		{"type": string(NotifIssueStateChanged), "description": "Issue state changed"},
		{"type": string(NotifStatusChangeRequest), "description": "Status change requested"},
		{"type": string(NotifProposalCreated), "description": "New proposal created"},
		{"type": string(NotifProposalStateChanged), "description": "Proposal state changed"},
		{"type": string(NotifTaskCreated), "description": "New task created"},
		{"type": string(NotifTaskAssigned), "description": "Task assigned to you"},
		{"type": string(NotifTaskStateChanged), "description": "Task state changed"},
		{"type": string(NotifAgentStatusChanged), "description": "Agent status changed"},
		{"type": string(NotifFeedbackReceived), "description": "Feedback received"},
	}
}

// GetSettings returns all notification settings for an agent.
func (s *Service) GetSettings(agentID uint) ([]models.NotificationSetting, error) {
	if s.db == nil {
		return nil, nil
	}
	var settings []models.NotificationSetting
	err := s.db.Where("agent_id = ?", agentID).Find(&settings).Error
	return settings, err
}

// UpdateSetting creates or updates a notification setting for an agent.
func (s *Service) UpdateSetting(agentID uint, notifType string, enabled bool, channel string) error {
	if s.db == nil {
		return fmt.Errorf("database not available")
	}
	if channel == "" {
		channel = "in_app"
	}
	return s.db.Where("agent_id = ? AND notification_type = ?", agentID, notifType).
		Assign(&models.NotificationSetting{
			AgentID:          agentID,
			NotificationType: notifType,
			Enabled:          enabled,
			Channel:          channel,
		}).
		FirstOrCreate(&models.NotificationSetting{}).Error
}

// IsEnabled checks if a notification type is enabled for an agent.
// Returns true by default if no setting is configured.
func (s *Service) IsEnabled(agentID uint, notifType NotificationType) bool {
	if s.db == nil {
		return true
	}
	var setting models.NotificationSetting
	err := s.db.Where("agent_id = ? AND notification_type = ?", agentID, string(notifType)).First(&setting).Error
	if err != nil {
		return true // enabled by default
	}
	return setting.Enabled
}
