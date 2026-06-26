package service

import (
	"fmt"
	"log"
	"time"

	"chick/internal/events"
	"chick/internal/models"
	"chick/internal/repository"
	gormrepo "chick/internal/repository/gorm"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TaskService struct {
	db           *gorm.DB
	taskRepo     repository.TaskRepository
	timelineRepo repository.TimelineRepository
	eventBus     *events.Bus
}

func NewTaskService(
	db *gorm.DB,
	taskRepo repository.TaskRepository,
	timelineRepo repository.TimelineRepository,
	eventBus *events.Bus,
) *TaskService {
	return &TaskService{
		db:           db,
		taskRepo:     taskRepo,
		timelineRepo: timelineRepo,
		eventBus:     eventBus,
	}
}

var validTaskTransitions = map[models.TaskState][]models.TaskState{
	models.TaskStatePending:    {models.TaskStateInProgress, models.TaskStateCancelled},
	models.TaskStateInProgress: {models.TaskStateCompleted, models.TaskStateBlocked, models.TaskStateCancelled},
	models.TaskStateBlocked:    {models.TaskStateInProgress, models.TaskStateCancelled},
	models.TaskStateCompleted:  {},
	models.TaskStateCancelled:  {},
}

func (s *TaskService) ValidTransitions(state models.TaskState) ([]models.TaskState, error) {
	allowed, ok := validTaskTransitions[state]
	if !ok {
		return nil, fmt.Errorf("unknown task state: %s", state)
	}
	return allowed, nil
}

func (s *TaskService) Create(proposalID, projectID, authorID uint, title, description string, priority models.Priority, assigneeID *uint) (*models.Task, error) {
	var task *models.Task

	err := s.db.Transaction(func(tx *gorm.DB) error {
		txTaskRepo := gormrepo.NewTaskRepo(tx)

		task = &models.Task{
			ProposalID:  proposalID,
			Title:       title,
			Description: description,
			State:       models.TaskStatePending,
			Priority:    priority,
			AssigneeID:  assigneeID,
		}
		if err := txTaskRepo.Create(task); err != nil {
			return fmt.Errorf("create task: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Timeline event
	event := &models.TimelineEvent{
		TaskID:    &task.ID,
		ActorID:   authorID,
		EventType: models.EventTaskCreated,
	}
	if err := s.timelineRepo.Create(event); err != nil {
		log.Printf("[task] failed to create timeline event: %v", err)
	}

	if s.eventBus != nil {
		s.eventBus.Publish(events.Event{
			Type: events.EventTaskCreated,
			Payload: events.TaskCreatedPayload{
				TaskID:     task.ID,
				ProposalID: proposalID,
				ProjectID:  projectID,
			},
		})
	}

	return s.taskRepo.GetByID(task.ID)
}

func (s *TaskService) GetByID(id uint) (*models.Task, error) {
	return s.taskRepo.GetByID(id)
}

func (s *TaskService) List(filter models.TaskFilter) ([]models.Task, int64, error) {
	return s.taskRepo.List(filter)
}

func (s *TaskService) Assign(taskID, assigneeID uint) (*models.Task, error) {
	if err := s.taskRepo.Update(taskID, map[string]interface{}{"assignee_id": assigneeID}); err != nil {
		return nil, fmt.Errorf("assign task: %w", err)
	}
	return s.taskRepo.GetByID(taskID)
}

func (s *TaskService) LinkIssue(taskID, issueID uint) error {
	return s.taskRepo.LinkIssue(taskID, issueID)
}

func (s *TaskService) UnlinkIssue(taskID, issueID uint) error {
	return s.taskRepo.UnlinkIssue(taskID, issueID)
}

func (s *TaskService) TransitionState(id uint, newState models.TaskState, actorID uint) (*models.Task, error) {
	var oldState models.TaskState
	var proposalID uint

	err := s.db.Transaction(func(tx *gorm.DB) error {
		var current models.Task
		if err := tx.Model(&models.Task{}).Select("state,proposal_id").
			Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, id).Error; err != nil {
			return fmt.Errorf("get task: %w", err)
		}
		oldState = current.State
		proposalID = current.ProposalID

		allowed, ok := validTaskTransitions[current.State]
		if !ok {
			return fmt.Errorf("unknown current state: %s", current.State)
		}
		valid := false
		for _, s := range allowed {
			if s == newState {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("transition from %s to %s not allowed", current.State, newState)
		}

		txTaskRepo := gormrepo.NewTaskRepo(tx)
		if err := txTaskRepo.UpdateState(id, newState); err != nil {
			return err
		}

		now := time.Now()
		updates := map[string]interface{}{}
		if newState == models.TaskStateInProgress && current.StartedAt == nil {
			updates["started_at"] = now
		}
		if newState == models.TaskStateCompleted && current.CompletedAt == nil {
			updates["completed_at"] = now
		}
		if len(updates) > 0 {
			if err := tx.Model(&models.Task{}).Where("id = ?", id).Updates(updates).Error; err != nil {
				return fmt.Errorf("set timestamps: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	payload := models.JSONMap{"from": string(oldState), "to": string(newState)}
	event := &models.TimelineEvent{
		TaskID:    &id,
		ActorID:   actorID,
		EventType: models.EventTaskStateChanged,
		Payload:   payload,
	}
	if err := s.timelineRepo.Create(event); err != nil {
		log.Printf("[task] failed to create timeline event: %v", err)
	}

	if s.eventBus != nil {
		s.eventBus.Publish(events.Event{
			Type: events.EventTaskStateChanged,
			Payload: events.TaskStateChangedPayload{
				TaskID:     id,
				ProposalID: proposalID,
				From:       string(oldState),
				To:         string(newState),
				ActorID:    actorID,
			},
		})
	}

	return s.taskRepo.GetByID(id)
}

func (s *TaskService) Update(id uint, changes map[string]interface{}) error {
	return s.taskRepo.Update(id, changes)
}

func (s *TaskService) Delete(id uint) error {
	return s.taskRepo.Delete(id)
}

func (s *TaskService) ListTimeline(taskID uint) ([]models.TimelineEvent, error) {
	return s.timelineRepo.ListByTask(taskID)
}
