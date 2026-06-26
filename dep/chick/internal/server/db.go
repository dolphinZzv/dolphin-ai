package server

import (
	"context"
	"fmt"
	"log"

	"strings"

	"chick/internal/config"
	"chick/internal/models"

	"github.com/redis/go-redis/v9"
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

func NewRedis(cfg *config.Config) *redis.Client {
	if cfg.RedisAddr == "" {
		return nil
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	// Verify connection
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Printf("[redis] connection failed: %v, falling back to in-memory", err)
		return nil
	}
	log.Printf("[redis] connected at %s (db %d)", cfg.RedisAddr, cfg.RedisDB)
	return rdb
}

func AutoMigrate(db *gorm.DB) error {
	// Drop old single-column unique index on issues.number (rename to composite)
	if strings.HasPrefix(db.Dialector.Name(), "sqlite") {
		db.Exec("DROP INDEX IF EXISTS idx_project_number")
	} else {
		db.Exec("DROP INDEX IF EXISTS idx_issues_project_number")
	}

	// Drop old single-column unique index on tasks.number (must include proposal_id)
	db.Exec("DROP INDEX IF EXISTS idx_tasks_proposal_number")

	err := db.AutoMigrate(
		&models.Project{},
		&models.ProjectMember{},
		&models.Agent{},
		&models.Issue{},
		&models.IssueAssignee{},
		&models.Comment{},
		&models.Label{},
		&models.Milestone{},
		&models.TimelineEvent{},
		&models.Feedback{},
		&models.Proposal{},
		&models.Task{},
		&models.NotificationSetting{},
	)
	if err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}
	log.Println("[db] auto migrate completed")
	return nil
}
