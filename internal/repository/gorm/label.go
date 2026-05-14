package gorm

import (
	"chick/internal/models"
	"chick/internal/repository"

	"gorm.io/gorm"
)

type LabelRepo struct {
	db *gorm.DB
}

func NewLabelRepo(db *gorm.DB) repository.LabelRepository {
	return &LabelRepo{db: db}
}

func (r *LabelRepo) Create(label *models.Label) error {
	return r.db.Create(label).Error
}

func (r *LabelRepo) GetByID(id uint) (*models.Label, error) {
	var l models.Label
	err := r.db.First(&l, id).Error
	return &l, err
}

func (r *LabelRepo) ListByProject(projectID uint, group string) ([]models.Label, error) {
	var list []models.Label
	q := r.db.Where("project_id = ?", projectID)
	if group != "" {
		q = q.Where("group = ?", group)
	}
	err := q.Find(&list).Error
	return list, err
}

func (r *LabelRepo) Update(id uint, changes map[string]interface{}) error {
	return r.db.Model(&models.Label{}).Where("id = ?", id).Updates(changes).Error
}

func (r *LabelRepo) Delete(id uint) error {
	return r.db.Delete(&models.Label{}, id).Error
}

type MilestoneRepo struct {
	db *gorm.DB
}

func NewMilestoneRepo(db *gorm.DB) repository.MilestoneRepository {
	return &MilestoneRepo{db: db}
}

func (r *MilestoneRepo) Create(milestone *models.Milestone) error {
	return r.db.Create(milestone).Error
}

func (r *MilestoneRepo) GetByID(id uint) (*models.Milestone, error) {
	var m models.Milestone
	err := r.db.First(&m, id).Error
	return &m, err
}

func (r *MilestoneRepo) ListByProject(projectID uint, state models.MilestoneState) ([]models.Milestone, error) {
	var list []models.Milestone
	q := r.db.Where("project_id = ?", projectID)
	if state != "" {
		q = q.Where("state = ?", state)
	}
	err := q.Find(&list).Error
	return list, err
}

func (r *MilestoneRepo) Update(id uint, changes map[string]interface{}) error {
	return r.db.Model(&models.Milestone{}).Where("id = ?", id).Updates(changes).Error
}

func (r *MilestoneRepo) Delete(id uint) error {
	return r.db.Delete(&models.Milestone{}, id).Error
}

type TimelineRepo struct {
	db *gorm.DB
}

func NewTimelineRepo(db *gorm.DB) repository.TimelineRepository {
	return &TimelineRepo{db: db}
}

func (r *TimelineRepo) Create(event *models.TimelineEvent) error {
	return r.db.Create(event).Error
}

func (r *TimelineRepo) ListByIssue(issueID uint) ([]models.TimelineEvent, error) {
	var list []models.TimelineEvent
	err := r.db.Where("issue_id = ?", issueID).
		Order("created_at ASC").
		Preload("Actor").
		Find(&list).Error
	return list, err
}

type FeedbackRepo struct {
	db *gorm.DB
}

func NewFeedbackRepo(db *gorm.DB) repository.FeedbackRepository {
	return &FeedbackRepo{db: db}
}

func (r *FeedbackRepo) Create(feedback *models.Feedback) error {
	return r.db.Create(feedback).Error
}

func (r *FeedbackRepo) ListByTarget(targetType models.FeedbackTargetType, targetID uint) ([]models.Feedback, error) {
	var list []models.Feedback
	err := r.db.Where("target_type = ? AND target_id = ?", targetType, targetID).
		Preload("Author").Find(&list).Error
	return list, err
}
