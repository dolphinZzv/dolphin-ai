package notifications_test

import (
	"testing"

	"chick/internal/events"
	"chick/internal/notifications"
)

func TestSubscribeAndNotify(t *testing.T) {
	bus := events.NewBus()
	svc := notifications.NewService(nil, nil)
	svc.Subscribe(bus)

	// Publish an assignee changed event
	bus.PublishSync(events.Event{
		Type: events.EventIssueAssigneeChanged,
		Payload: events.IssueAssigneeChangedPayload{
			IssueID: 42,
			AgentID: 1,
			Action:  "assigned",
		},
	})

	// Publish a comment event
	bus.PublishSync(events.Event{
		Type: events.EventCommentAdded,
		Payload: events.CommentAddedPayload{
			CommentID: 10,
			IssueID:   42,
			AuthorID:  2,
		},
	})

	// Check agent 1's notifications
	notifs := svc.ListByAgent(1)
	if len(notifs) != 2 {
		t.Errorf("expected 2 notifications, got %d", len(notifs))
	}

	if len(notifs) > 0 {
		if notifs[0].Type != notifications.NotifCommentMention {
			t.Errorf("expected newest to be comment_mention, got %s", notifs[0].Type)
		}
	}
}

func TestListByAgent_FiltersCorrectly(t *testing.T) {
	svc := notifications.NewService(nil, nil)

	// Create some notifications directly
	bus := events.NewBus()
	svc.Subscribe(bus)

	bus.PublishSync(events.Event{
		Type: events.EventIssueAssigneeChanged,
		Payload: events.IssueAssigneeChangedPayload{
			IssueID: 1,
			AgentID: 5,
			Action:  "assigned",
		},
	})

	bus.PublishSync(events.Event{
		Type: events.EventIssueAssigneeChanged,
		Payload: events.IssueAssigneeChangedPayload{
			IssueID: 2,
			AgentID: 3,
			Action:  "assigned",
		},
	})

	// Agent 5 should see 1 notification
	notifs5 := svc.ListByAgent(5)
	if len(notifs5) != 1 {
		t.Errorf("agent 5 expected 1 notification, got %d", len(notifs5))
	}

	// Agent 3 should see 1 notification
	notifs3 := svc.ListByAgent(3)
	if len(notifs3) != 1 {
		t.Errorf("agent 3 expected 1 notification, got %d", len(notifs3))
	}
}

func TestMarkRead(t *testing.T) {
	svc := notifications.NewService(nil, nil)

	bus := events.NewBus()
	svc.Subscribe(bus)

	bus.PublishSync(events.Event{
		Type: events.EventIssueAssigneeChanged,
		Payload: events.IssueAssigneeChangedPayload{
			IssueID: 1,
			AgentID: 1,
			Action:  "assigned",
		},
	})

	notifs := svc.ListByAgent(1)
	if len(notifs) == 0 {
		t.Fatal("expected notification")
	}

	if notifs[0].Read {
		t.Error("expected notification to be unread")
	}

	err := svc.MarkRead(notifs[0].ID)
	if err != nil {
		t.Fatalf("mark read: %v", err)
	}

	notifs = svc.ListByAgent(1)
	if !notifs[0].Read {
		t.Error("expected notification to be read after MarkRead")
	}
}
