package gorm

import (
	"chick/internal/models"
	"chick/internal/repository"

	"gorm.io/gorm"
)

type IssueRepo struct {
	db *gorm.DB
}

func NewIssueRepo(db *gorm.DB) repository.IssueRepository {
	return &IssueRepo{db: db}
}

func (r *IssueRepo) Create(issue *models.Issue) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var max struct {
			Number uint
		}
		if err := tx.Model(&models.Issue{}).
			Select("COALESCE(MAX(number), 0) as number").
			Where("project_id = ?", issue.ProjectID).
			Scan(&max).Error; err != nil {
			return err
		}
		issue.Number = max.Number + 1
		return tx.Create(issue).Error
	})
}

func (r *IssueRepo) GetByID(id uint) (*models.Issue, error) {
	var i models.Issue
	err := r.db.Preload("Assignees.Agent").Preload("Labels").Preload("Comments").Preload("Comments.Author").Preload("Creator").Preload("Milestone").
		First(&i, id).Error
	return &i, err
}

func (r *IssueRepo) GetByNumber(projectID uint, number uint) (*models.Issue, error) {
	var i models.Issue
	err := r.db.Where("project_id = ? AND number = ?", projectID, number).
		Preload("Assignees.Agent").Preload("Labels").Preload("Creator").
		First(&i).Error
	return &i, err
}

func (r *IssueRepo) List(filter models.IssueFilter) ([]models.Issue, int64, error) {
	q := r.db.Model(&models.Issue{})
	if filter.ProjectID != nil {
		q = q.Where("project_id = ?", *filter.ProjectID)
	}
	if len(filter.State) > 0 {
		q = q.Where("state IN ?", filter.State)
	}
	if len(filter.AssigneeIDs) > 0 {
		q = q.Joins("JOIN issue_assignees ON issue_assignees.issue_id = issues.id").
			Where("issue_assignees.agent_id IN ?", filter.AssigneeIDs)
	}
	if filter.MilestoneID != nil {
		q = q.Where("milestone_id = ?", *filter.MilestoneID)
	}
	if filter.Priority != nil {
		q = q.Where("priority = ?", *filter.Priority)
	}
	if filter.CreatorID != nil {
		q = q.Where("creator_id = ?", *filter.CreatorID)
	}
	if len(filter.LabelIDs) > 0 {
		q = q.Joins("JOIN issue_labels ON issue_labels.issue_id = issues.id").
			Where("issue_labels.label_id IN ?", filter.LabelIDs)
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

	var issues []models.Issue
	err := q.Preload("Assignees.Agent").Preload("Labels").Preload("Creator").Find(&issues).Error
	return issues, total, err
}

func (r *IssueRepo) AddLabel(issueID, labelID uint) error {
	return r.db.Table("issue_labels").Create(map[string]interface{}{
		"issue_id": issueID,
		"label_id": labelID,
	}).Error
}

func (r *IssueRepo) RemoveLabel(issueID, labelID uint) error {
	return r.db.Table("issue_labels").
		Where("issue_id = ? AND label_id = ?", issueID, labelID).
		Delete(nil).Error
}

func (r *IssueRepo) UpdateState(id uint, state models.IssueState) error {
	return r.db.Model(&models.Issue{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"state": state,
		}).Error
}

func (r *IssueRepo) Update(id uint, changes map[string]interface{}) error {
	return r.db.Model(&models.Issue{}).Where("id = ?", id).Updates(changes).Error
}

func (r *IssueRepo) Delete(id uint) error {
	return r.db.Delete(&models.Issue{}, id).Error
}

func (r *IssueRepo) NextNumber(projectID uint) (uint, error) {
	var max struct {
		Number uint
	}
	err := r.db.Model(&models.Issue{}).
		Select("COALESCE(MAX(number), 0) as number").
		Where("project_id = ?", projectID).
		Scan(&max).Error
	return max.Number + 1, err
}

func (r *IssueRepo) Transaction(fn func(db *gorm.DB) error) error {
	return r.db.Transaction(fn)
}
