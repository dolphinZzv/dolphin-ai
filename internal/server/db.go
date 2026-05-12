package server

import (
	"fmt"
	"log"

	"chick/internal/config"
	"chick/internal/models"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func NewDB(cfg *config.Config) (*gorm.DB, error) {
	var dial gorm.Dialector
	if cfg.UsePostgreSQL() {
		dial = postgres.Open(cfg.DBDSN)
	} else {
		dial = sqlite.Open(cfg.DBDSN)
	}

	db, err := gorm.Open(dial, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("open db (%s): %w", cfg.DBDriver, err)
	}

	return db, nil
}

func AutoMigrate(db *gorm.DB) error {
	// Drop old single-column unique index on issues.number (rename to composite)
	db.Exec("DROP INDEX IF EXISTS idx_project_number")

	err := db.AutoMigrate(
		&models.Project{},
		&models.ProjectMember{},
		&models.Agent{},
		&models.Issue{},
		&models.IssueAssignee{},
		&models.Comment{},
		&models.Label{},
		&models.Milestone{},
		&models.Skill{},
		&models.TimelineEvent{},
		&models.Feedback{},
	)
	if err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}
	log.Println("[db] auto migrate completed")
	return nil
}
