package graph

import (
	"context"
	"fmt"
	"strconv"

	"chick/internal/notifications"
)

// NotificationSettings is the resolver for the notificationSettings field.
func (r *queryResolver) NotificationSettings(ctx context.Context, agentID string) ([]*NotificationSetting, error) {
	pid := parseID(agentID)
	settings, err := r.NotifSvc.GetSettings(pid)
	if err != nil {
		return nil, fmt.Errorf("get notification settings: %w", err)
	}

	result := make([]*NotificationSetting, len(settings))
	for i, s := range settings {
		result[i] = &NotificationSetting{
			ID:               strconv.FormatUint(uint64(s.ID), 10),
			AgentID:          strconv.FormatUint(uint64(s.AgentID), 10),
			NotificationType: s.NotificationType,
			Enabled:          s.Enabled,
			Channel:          s.Channel,
		}
	}
	return result, nil
}

// NotificationTypes is the resolver for the notificationTypes field.
func (r *queryResolver) NotificationTypes(ctx context.Context) ([]*NotificationTypeInfo, error) {
	types := notifications.AllNotificationTypes()
	result := make([]*NotificationTypeInfo, len(types))
	for i, t := range types {
		result[i] = &NotificationTypeInfo{
			Type:        t["type"],
			Description: t["description"],
		}
	}
	return result, nil
}

// UpdateNotificationSetting is the resolver for the updateNotificationSetting field.
func (r *mutationResolver) UpdateNotificationSetting(ctx context.Context, agentID string, notificationType string, enabled bool, channel *string) (*NotificationSetting, error) {
	pid := parseID(agentID)
	ch := ""
	if channel != nil {
		ch = *channel
	}
	if err := r.NotifSvc.UpdateSetting(pid, notificationType, enabled, ch); err != nil {
		return nil, fmt.Errorf("update notification setting: %w", err)
	}

	// Read back the saved setting
	settings, err := r.NotifSvc.GetSettings(pid)
	if err != nil {
		return nil, fmt.Errorf("get notification settings: %w", err)
	}
	for _, s := range settings {
		if s.NotificationType == notificationType {
			return &NotificationSetting{
				ID:               strconv.FormatUint(uint64(s.ID), 10),
				AgentID:          strconv.FormatUint(uint64(s.AgentID), 10),
				NotificationType: s.NotificationType,
				Enabled:          s.Enabled,
				Channel:          s.Channel,
			}, nil
		}
	}

	return nil, fmt.Errorf("notification setting not found after update")
}
