package gorm

import (
	"chick/internal/models"
	"chick/internal/repository"

	"gorm.io/gorm"
)

type ProjectRepo struct {
	db *gorm.DB
}

func NewProjectRepo(db *gorm.DB) repository.ProjectRepository {
	return &ProjectRepo{db: db}
}

func (r *ProjectRepo) Create(project *models.Project) error {
	return r.db.Create(project).Error
}

func (r *ProjectRepo) GetByID(id uint) (*models.Project, error) {
	var p models.Project
	err := r.db.Preload("Members.Agent").First(&p, id).Error
	return &p, err
}

func (r *ProjectRepo) Update(id uint, changes map[string]interface{}) error {
	return r.db.Model(&models.Project{}).Where("id = ?", id).Updates(changes).Error
}

func (r *ProjectRepo) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// Delete feedback referencing project issues
		if err := tx.Where("target_type = 'issue' AND target_id IN (SELECT id FROM issues WHERE project_id = ?)", id).Delete(&models.Feedback{}).Error; err != nil {
			return err
		}
		// Delete timeline events for project issues
		if err := tx.Where("issue_id IN (SELECT id FROM issues WHERE project_id = ?)", id).Delete(&models.TimelineEvent{}).Error; err != nil {
			return err
		}
		// Delete comments for project issues
		if err := tx.Where("issue_id IN (SELECT id FROM issues WHERE project_id = ?)", id).Delete(&models.Comment{}).Error; err != nil {
			return err
		}
		// Delete issue_assignees for project issues
		if err := tx.Where("issue_id IN (SELECT id FROM issues WHERE project_id = ?)", id).Delete(&models.IssueAssignee{}).Error; err != nil {
			return err
		}
		// Delete issue_labels for project issues
		if err := tx.Exec("DELETE FROM issue_labels WHERE issue_id IN (SELECT id FROM issues WHERE project_id = ?)", id).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.Issue{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.Label{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.Milestone{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.ProjectMember{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.Project{}, id).Error
	})
}

func (r *ProjectRepo) List() ([]models.Project, error) {
	var list []models.Project
	err := r.db.Find(&list).Error
	return list, err
}
