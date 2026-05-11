package notifications

import (
	"fmt"
	"log"
	"sync"
	"time"

	"chick/internal/events"
)

type NotificationType string

const (
	NotifIssueAssigned      NotificationType = "issue_assigned"
	NotifCommentMention     NotificationType = "comment_mention"
	NotifIssueStateChanged  NotificationType = "issue_state_changed"
	NotifStatusChangeRequest NotificationType = "status_change_request"
)

type Notification struct {
	ID        uint             `json:"id"`
	AgentID   uint             `json:"agentId"`
	Type      NotificationType `json:"type"`
	IssueID   uint             `json:"issueId,omitempty"`
	CommentID uint             `json:"commentId,omitempty"`
	FromID    uint             `json:"fromId,omitempty"`
	Message   string           `json:"message"`
	Read      bool             `json:"read"`
	CreatedAt time.Time        `json:"createdAt"`
}

type Service struct {
	mu            sync.RWMutex
	notifications []Notification
	nextID        uint
}

func NewService() *Service {
	return &Service{
		notifications: make([]Notification, 0),
		nextID:        1,
	}
}

// Subscribe registers all notification handlers on the event bus.
func (s *Service) Subscribe(bus *events.Bus) {
	bus.Subscribe(events.EventIssueCreated, s.handleIssueCreated)
	bus.Subscribe(events.EventIssueAssigneeChanged, s.handleAssigneeChanged)
	bus.Subscribe(events.EventIssueStateChanged, s.handleStateChanged)
	bus.Subscribe(events.EventCommentAdded, s.handleCommentAdded)
	log.Println("[notifications] subscribed to events")
}

func (s *Service) handleIssueCreated(evt events.Event) {
	payload, ok := evt.Payload.(map[string]interface{})
	if !ok {
		return
	}
	projectID, _ := payload["projectID"].(uint)
	issueID, _ := payload["issueID"].(uint)

	s.add(Notification{
		AgentID:   0, // broadcast to project
		Type:      NotifIssueAssigned,
		IssueID:   issueID,
		Message:   fmt.Sprintf("New issue created in project %d", projectID),
		CreatedAt: time.Now(),
	})
}

func (s *Service) handleAssigneeChanged(evt events.Event) {
	payload, ok := evt.Payload.(map[string]interface{})
	if !ok {
		return
	}
	issueID, _ := payload["issueID"].(uint)
	agentID, _ := payload["agentID"].(uint)
	action, _ := payload["action"].(string)

	if action == "assigned" {
		s.add(Notification{
			AgentID:   agentID,
			Type:      NotifIssueAssigned,
			IssueID:   issueID,
			Message:   fmt.Sprintf("You have been assigned to issue %d", issueID),
			CreatedAt: time.Now(),
		})
	}
}

func (s *Service) handleStateChanged(evt events.Event) {
	payload, ok := evt.Payload.(map[string]interface{})
	if !ok {
		return
	}
	issueID, _ := payload["issueID"].(uint)
	to, _ := payload["to"].(string)

	s.add(Notification{
		Type:      NotifIssueStateChanged,
		IssueID:   issueID,
		Message:   fmt.Sprintf("Issue %d changed to %s", issueID, to),
		CreatedAt: time.Now(),
	})
}

func (s *Service) handleCommentAdded(evt events.Event) {
	payload, ok := evt.Payload.(map[string]interface{})
	if !ok {
		return
	}
	commentID, _ := payload["commentID"].(uint)
	issueID, _ := payload["issueID"].(uint)

	s.add(Notification{
		Type:      NotifCommentMention,
		IssueID:   issueID,
		CommentID: commentID,
		Message:   fmt.Sprintf("New comment on issue %d", issueID),
		CreatedAt: time.Now(),
	})
}

// ListByAgent returns notifications for a specific agent, newest first.
func (s *Service) ListByAgent(agentID uint) []Notification {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Notification
	for _, n := range s.notifications {
		if n.AgentID == 0 || n.AgentID == agentID {
			result = append(result, n)
		}
	}
	// Return newest first (last elements)
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

// MarkRead marks a notification as read.
func (s *Service) MarkRead(id uint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.notifications {
		if s.notifications[i].ID == id {
			s.notifications[i].Read = true
			return nil
		}
	}
	return fmt.Errorf("notification %d not found", id)
}

func (s *Service) add(n Notification) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n.ID = s.nextID
	s.nextID++
	s.notifications = append(s.notifications, n)
}
