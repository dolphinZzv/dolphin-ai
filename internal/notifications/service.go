package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"chick/internal/events"

	"github.com/redis/go-redis/v9"
)

type NotificationType string

const (
	NotifIssueAssigned       NotificationType = "issue_assigned"
	NotifCommentMention      NotificationType = "comment_mention"
	NotifIssueStateChanged   NotificationType = "issue_state_changed"
	NotifStatusChangeRequest NotificationType = "status_change_request"
)

type Notification struct {
	ID        uint             `json:"id"`
	AgentID   uint             `json:"agentId"`
	ProjectID uint             `json:"projectId,omitempty"`
	Type      NotificationType `json:"type"`
	IssueID   uint             `json:"issueId,omitempty"`
	CommentID uint             `json:"commentId,omitempty"`
	FromID    uint             `json:"fromId,omitempty"`
	Message   string           `json:"message"`
	Read      bool             `json:"read"`
	CreatedAt time.Time        `json:"createdAt"`
}

type Service struct {
	rdb *redis.Client

	// fallback in-memory storage when rdb is nil
	mu            sync.RWMutex
	notifications []Notification
	nextID        uint
}

const broadcastAgentID uint = 0

func NewService(rdb *redis.Client) *Service {
	return &Service{
		rdb:           rdb,
		notifications: make([]Notification, 0),
		nextID:        1,
	}
}

func notifDataKey(id uint) string   { return fmt.Sprintf("notif:d:%d", id) }
func agentSetKey(agentID uint) string  { return fmt.Sprintf("notif:s:agent:%d", agentID) }
func broadcastSetKey() string          { return "notif:s:broadcast" }
func notifIDKey() string               { return "notif:id" }

// Subscribe registers all notification handlers on the event bus.
func (s *Service) Subscribe(bus *events.Bus) {
	bus.Subscribe(events.EventIssueCreated, s.handleIssueCreated)
	bus.Subscribe(events.EventIssueAssigneeChanged, s.handleAssigneeChanged)
	bus.Subscribe(events.EventIssueStateChanged, s.handleStateChanged)
	bus.Subscribe(events.EventCommentAdded, s.handleCommentAdded)
	log.Println("[notifications] subscribed to events")
}

func (s *Service) handleIssueCreated(evt events.Event) {
	payload, ok := evt.Payload.(events.IssueCreatedPayload)
	if !ok {
		return
	}

	for _, aid := range payload.AssigneeIDs {
		s.add(Notification{
			ProjectID: payload.ProjectID,
			AgentID:   aid,
			Type:      NotifIssueAssigned,
			IssueID:   payload.IssueID,
			Message:   fmt.Sprintf("You have been assigned to issue #%d", payload.IssueID),
			CreatedAt: time.Now(),
		})
	}

	s.add(Notification{
		ProjectID: payload.ProjectID,
		AgentID:   broadcastAgentID,
		Type:      NotifIssueAssigned,
		IssueID:   payload.IssueID,
		Message:   fmt.Sprintf("New issue created in project %d", payload.ProjectID),
		CreatedAt: time.Now(),
	})
}

func (s *Service) handleAssigneeChanged(evt events.Event) {
	payload, ok := evt.Payload.(events.IssueAssigneeChangedPayload)
	if !ok {
		return
	}

	if payload.Action == "assigned" {
		s.add(Notification{
			ProjectID: payload.ProjectID,
			AgentID:   payload.AgentID,
			Type:      NotifIssueAssigned,
			IssueID:   payload.IssueID,
			Message:   fmt.Sprintf("You have been assigned to issue %d", payload.IssueID),
			CreatedAt: time.Now(),
		})
	}
}

func (s *Service) handleStateChanged(evt events.Event) {
	payload, ok := evt.Payload.(events.IssueStateChangedPayload)
	if !ok {
		return
	}

	s.add(Notification{
		ProjectID: payload.ProjectID,
		Type:      NotifIssueStateChanged,
		IssueID:   payload.IssueID,
		Message:   fmt.Sprintf("Issue %d changed to %s", payload.IssueID, payload.To),
		CreatedAt: time.Now(),
	})
}

func (s *Service) handleCommentAdded(evt events.Event) {
	payload, ok := evt.Payload.(events.CommentAddedPayload)
	if !ok {
		return
	}

	s.add(Notification{
		ProjectID: payload.ProjectID,
		Type:      NotifCommentMention,
		IssueID:   payload.IssueID,
		CommentID: payload.CommentID,
		Message:   fmt.Sprintf("New comment on issue %d", payload.IssueID),
		CreatedAt: time.Now(),
	})
}

// ListByAgent returns notifications for a specific agent, optionally filtered by project.
func (s *Service) ListByAgent(agentID uint, projectID ...uint) []Notification {
	if s.rdb != nil {
		return s.listByAgentRedis(agentID, projectID...)
	}
	return s.listByAgentMem(agentID, projectID...)
}

func (s *Service) listByAgentMem(agentID uint, projectID ...uint) []Notification {
	s.mu.RLock()
	defer s.mu.RUnlock()

	filterProject := len(projectID) > 0 && projectID[0] > 0

	var result []Notification
	for _, n := range s.notifications {
		if n.AgentID != 0 && n.AgentID != agentID {
			continue
		}
		if filterProject && n.ProjectID != projectID[0] {
			continue
		}
		result = append(result, n)
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}

func (s *Service) listByAgentRedis(agentID uint, projectID ...uint) []Notification {
	ctx := context.Background()
	filterProject := len(projectID) > 0 && projectID[0] > 0

	// Get IDs from both agent set and broadcast set
	agentIDs, _ := s.rdb.ZRevRange(ctx, agentSetKey(agentID), 0, -1).Result()
	bcastIDs, _ := s.rdb.ZRevRange(ctx, broadcastSetKey(), 0, -1).Result()

	allIDs := make([]string, 0, len(agentIDs)+len(bcastIDs))
	allIDs = append(allIDs, agentIDs...)
	allIDs = append(allIDs, bcastIDs...)

	if len(allIDs) == 0 {
		return nil
	}

	// Fetch all notification data in one pipeline
	pipe := s.rdb.Pipeline()
	cmds := make([]*redis.StringCmd, len(allIDs))
	for i, id := range allIDs {
		cmds[i] = pipe.Get(ctx, notifDataKey(parseID(id)))
	}
	pipe.Exec(ctx)

	notifs := make([]Notification, 0, len(allIDs))
	for _, cmd := range cmds {
		data, err := cmd.Result()
		if err != nil {
			continue
		}
		var n Notification
		if err := json.Unmarshal([]byte(data), &n); err != nil {
			continue
		}
		if filterProject && n.ProjectID != projectID[0] {
			continue
		}
		notifs = append(notifs, n)
	}

	// Sort by CreatedAt descending (since ZRevRange may interleave)
	sort.Slice(notifs, func(i, j int) bool {
		return notifs[i].CreatedAt.After(notifs[j].CreatedAt)
	})

	return notifs
}

// MarkRead marks a notification as read.
func (s *Service) MarkRead(id uint) error {
	if s.rdb != nil {
		return s.markReadRedis(id)
	}
	return s.markReadMem(id)
}

func (s *Service) markReadMem(id uint) error {
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

func (s *Service) markReadRedis(id uint) error {
	ctx := context.Background()
	key := notifDataKey(id)
	data, err := s.rdb.Get(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("notification %d not found", id)
	}
	var n Notification
	if err := json.Unmarshal([]byte(data), &n); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	n.Read = true
	updated, _ := json.Marshal(n)
	return s.rdb.Set(ctx, key, updated, 0).Err()
}

// add stores a notification.
func (s *Service) add(n Notification) {
	n.CreatedAt = time.Now()
	if s.rdb != nil {
		s.addRedis(n)
		return
	}
	s.addMem(n)
}

func (s *Service) addMem(n Notification) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n.ID = s.nextID
	s.nextID++
	s.notifications = append(s.notifications, n)
}

func (s *Service) addRedis(n Notification) {
	ctx := context.Background()
	id := uint(s.rdb.Incr(ctx, notifIDKey()).Val())
	n.ID = id

	data, err := json.Marshal(n)
	if err != nil {
		log.Printf("[notifications] marshal error: %v", err)
		return
	}

	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, notifDataKey(id), data, 0)
	score := float64(n.CreatedAt.UnixNano())

	if n.AgentID == broadcastAgentID {
		pipe.ZAdd(ctx, broadcastSetKey(), redis.Z{Score: score, Member: fmt.Sprint(id)})
	} else {
		pipe.ZAdd(ctx, agentSetKey(n.AgentID), redis.Z{Score: score, Member: fmt.Sprint(id)})
	}
	pipe.Exec(ctx)
}

func parseID(s string) uint {
	var n uint
	for _, c := range s {
		n = n*10 + uint(c-'0')
	}
	return n
}
