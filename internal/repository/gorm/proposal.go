package gorm

import (
	"chick/internal/models"
	"chick/internal/repository"

	"gorm.io/gorm"
)

type ProposalRepo struct {
	db *gorm.DB
}

func NewProposalRepo(db *gorm.DB) repository.ProposalRepository {
	return &ProposalRepo{db: db}
}

func (r *ProposalRepo) Create(proposal *models.Proposal) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var max struct {
			Number uint
		}
		if err := tx.Model(&models.Proposal{}).
			Select("COALESCE(MAX(number), 0) as number").
			Where("project_id = ?", proposal.ProjectID).
			Scan(&max).Error; err != nil {
			return err
		}
		proposal.Number = max.Number + 1
		return tx.Create(proposal).Error
	})
}

func (r *ProposalRepo) GetByID(id uint) (*models.Proposal, error) {
	var p models.Proposal
	err := r.db.Preload("Author").Preload("Reviewer").Preload("Labels").
		Preload("Tasks", func(db *gorm.DB) *gorm.DB {
			return db.Order("number ASC")
		}).
		First(&p, id).Error
	return &p, err
}

func (r *ProposalRepo) List(filter models.ProposalFilter) ([]models.Proposal, int64, error) {
	q := r.db.Model(&models.Proposal{})
	if filter.ProjectID != nil {
		q = q.Where("project_id = ?", *filter.ProjectID)
	}
	if len(filter.State) > 0 {
		q = q.Where("state IN ?", filter.State)
	}
	if filter.Priority != nil {
		q = q.Where("priority = ?", *filter.Priority)
	}
	if filter.AuthorID != nil {
		q = q.Where("author_id = ?", *filter.AuthorID)
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

	var proposals []models.Proposal
	err := q.Preload("Author").Preload("Reviewer").Preload("Labels").Preload("Tasks", func(db *gorm.DB) *gorm.DB {
			return db.Order("number ASC")
		}).Find(&proposals).Error
	return proposals, total, err
}

func (r *ProposalRepo) UpdateState(id uint, state models.ProposalState) error {
	return r.db.Model(&models.Proposal{}).Where("id = ?", id).
		Update("state", state).Error
}

func (r *ProposalRepo) Update(id uint, changes map[string]interface{}) error {
	return r.db.Model(&models.Proposal{}).Where("id = ?", id).Updates(changes).Error
}

func (r *ProposalRepo) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// Delete dependent records
		if err := tx.Where("proposal_id = ?", id).Delete(&models.Task{}).Error; err != nil {
			return err
		}
		if err := tx.Where("proposal_id = ?", id).Delete(&models.TimelineEvent{}).Error; err != nil {
			return err
		}
		if err := tx.Where("proposal_id = ?", id).Delete(&models.Comment{}).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM proposal_labels WHERE proposal_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Delete(&models.Proposal{}, id).Error
	})
}

func (r *ProposalRepo) NextNumber(projectID uint) (uint, error) {
	var max struct {
		Number uint
	}
	err := r.db.Model(&models.Proposal{}).
		Select("COALESCE(MAX(number), 0) as number").
		Where("project_id = ?", projectID).
		Scan(&max).Error
	return max.Number + 1, err
}
