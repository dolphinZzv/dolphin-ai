package gorm

import (
	"chick/internal/models"
	"chick/internal/repository"

	"gorm.io/gorm"
)

type TaskRepo struct {
	db *gorm.DB
}

func NewTaskRepo(db *gorm.DB) repository.TaskRepository {
	return &TaskRepo{db: db}
}

func (r *TaskRepo) Create(task *models.Task) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var max struct {
			Number uint
		}
		if err := tx.Model(&models.Task{}).
			Select("COALESCE(MAX(number), 0) as number").
			Where("proposal_id = ?", task.ProposalID).
			Scan(&max).Error; err != nil {
			return err
		}
		task.Number = max.Number + 1
		return tx.Create(task).Error
	})
}

func (r *TaskRepo) GetByID(id uint) (*models.Task, error) {
	var t models.Task
	err := r.db.Preload("Assignee").Preload("Proposal").
		Preload("Issues", func(db *gorm.DB) *gorm.DB {
			return db.Order("issues.number ASC")
		}).
		First(&t, id).Error
	return &t, err
}

func (r *TaskRepo) List(filter models.TaskFilter) ([]models.Task, int64, error) {
	q := r.db.Model(&models.Task{})
	if filter.ProposalID != nil {
		q = q.Where("proposal_id = ?", *filter.ProposalID)
	}
	if len(filter.State) > 0 {
		q = q.Where("state IN ?", filter.State)
	}
	if filter.AssigneeID != nil {
		q = q.Where("assignee_id = ?", *filter.AssigneeID)
	}
	if filter.Priority != nil {
		q = q.Where("priority = ?", *filter.Priority)
	}
	if filter.Search != "" {
		q = q.Where("title LIKE ? OR description LIKE ?", "%"+filter.Search+"%", "%"+filter.Search+"%")
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if filter.OrderBy != "" {
		dir := "DESC"
		if filter.OrderDir == "ASC" {
			dir = "ASC"
		}
		q = q.Order(filter.OrderBy + " " + dir)
	} else {
		q = q.Order("created_at DESC")
	}
	if filter.Limit > 0 {
		q = q.Limit(filter.Limit).Offset(filter.Offset)
	}

	var tasks []models.Task
	err := q.Preload("Assignee").Find(&tasks).Error
	return tasks, total, err
}

func (r *TaskRepo) UpdateState(id uint, state models.TaskState) error {
	return r.db.Model(&models.Task{}).Where("id = ?", id).
		Update("state", state).Error
}

func (r *TaskRepo) Update(id uint, changes map[string]interface{}) error {
	return r.db.Model(&models.Task{}).Where("id = ?", id).Updates(changes).Error
}

func (r *TaskRepo) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("task_id = ?", id).Delete(&models.TimelineEvent{}).Error; err != nil {
			return err
		}
		if err := tx.Where("task_id = ?", id).Delete(&models.Comment{}).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM task_issues WHERE task_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Delete(&models.Task{}, id).Error
	})
}

func (r *TaskRepo) NextNumber(proposalID uint) (uint, error) {
	var max struct {
		Number uint
	}
	err := r.db.Model(&models.Task{}).
		Select("COALESCE(MAX(number), 0) as number").
		Where("proposal_id = ?", proposalID).
		Scan(&max).Error
	return max.Number + 1, err
}

func (r *TaskRepo) LinkIssue(taskID, issueID uint) error {
	return r.db.Table("task_issues").Create(map[string]interface{}{
		"task_id":  taskID,
		"issue_id": issueID,
	}).Error
}

func (r *TaskRepo) UnlinkIssue(taskID, issueID uint) error {
	return r.db.Table("task_issues").
		Where("task_id = ? AND issue_id = ?", taskID, issueID).
		Delete(nil).Error
}
