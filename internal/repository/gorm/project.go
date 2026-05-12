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

func (r *ProjectRepo) FindByBootstrapToken(token string) (*models.Project, error) {
	var p models.Project
	err := r.db.Where("bootstrap_token = ?", token).First(&p).Error
	return &p, err
}

func (r *ProjectRepo) Update(id uint, changes map[string]interface{}) error {
	return r.db.Model(&models.Project{}).Where("id = ?", id).Updates(changes).Error
}

func (r *ProjectRepo) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ?", id).Delete(&models.Issue{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.Label{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.Milestone{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.Skill{}).Error; err != nil {
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
