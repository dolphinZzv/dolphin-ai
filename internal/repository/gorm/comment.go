package gorm

import (
	"chick/internal/models"
	"chick/internal/repository"

	"gorm.io/gorm"
)

type CommentRepo struct {
	db *gorm.DB
}

func NewCommentRepo(db *gorm.DB) repository.CommentRepository {
	return &CommentRepo{db: db}
}

func (r *CommentRepo) Create(comment *models.Comment) error {
	return r.db.Create(comment).Error
}

func (r *CommentRepo) GetByID(id uint) (*models.Comment, error) {
	var c models.Comment
	err := r.db.Preload("Author").First(&c, id).Error
	return &c, err
}

func (r *CommentRepo) ListByIssue(issueID uint) ([]models.Comment, error) {
	var list []models.Comment
	err := r.db.Where("issue_id = ?", issueID).
		Order("created_at ASC").
		Preload("Author").
		Find(&list).Error
	return list, err
}

func (r *CommentRepo) ListByParent(parentID uint) ([]models.Comment, error) {
	var list []models.Comment
	err := r.db.Where("parent_id = ?", parentID).
		Order("created_at ASC").
		Preload("Author").
		Find(&list).Error
	return list, err
}

func (r *CommentRepo) Delete(id uint) error {
	return r.db.Delete(&models.Comment{}, id).Error
}
