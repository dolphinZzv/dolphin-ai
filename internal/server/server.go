package server

import (
	"log"

	"chick/internal/auth"
	"chick/internal/config"
	"chick/internal/events"
	"chick/internal/matching"
	"chick/internal/notifications"
	gormrepo "chick/internal/repository/gorm"
	"chick/internal/service"

	"gorm.io/gorm"
)

type Server struct {
	Config          *config.Config
	DB              *gorm.DB
	EventBus        *events.Bus

	ProjectService   *service.ProjectService
	AgentService     *service.AgentService
	IssueService     *service.IssueService
	CommentService   *service.CommentService
	WorkflowService  *service.WorkflowService
	FeedbackService  *service.FeedbackService
	Authenticator    *auth.Authenticator
	NotifService     *notifications.Service
	MatchingEngine   *matching.Engine
}

func New(cfg *config.Config) (*Server, error) {
	db, err := NewDB(cfg)
	if err != nil {
		return nil, err
	}

	if err := AutoMigrate(db); err != nil {
		return nil, err
	}

	bus := events.NewBus()

	// Init repositories
	projectRepo := gormrepo.NewProjectRepo(db)
	memberRepo := gormrepo.NewProjectMemberRepo(db)
	agentRepo := gormrepo.NewAgentRepo(db)
	issueRepo := gormrepo.NewIssueRepo(db)
	assigneeRepo := gormrepo.NewIssueAssigneeRepo(db)
	commentRepo := gormrepo.NewCommentRepo(db)
	timelineRepo := gormrepo.NewTimelineRepo(db)
	labelRepo := gormrepo.NewLabelRepo(db)
	milestoneRepo := gormrepo.NewMilestoneRepo(db)
	feedbackRepo := gormrepo.NewFeedbackRepo(db)

	// Init auth
	authn := auth.New(cfg.JWTSecret)

	// Init matching engine
	matchingEngine := matching.NewEngine(agentRepo, gormrepo.NewLabelRepo(db), assigneeRepo, issueRepo)
	matchingEngine.Subscribe(bus)

	// Init notification service
	notifSvc := notifications.NewService()
	notifSvc.Subscribe(bus)

	// Init services
	projectSvc := service.NewProjectService(projectRepo, memberRepo, labelRepo, milestoneRepo)
	agentSvc := service.NewAgentService(agentRepo, bus, authn, cfg.AllowHumanRegistration)
	commentSvc := service.NewCommentService(db, commentRepo, timelineRepo, issueRepo, bus)
	issueSvc := service.NewIssueService(db, issueRepo, assigneeRepo, timelineRepo, projectRepo, bus)
	workflowSvc := service.NewWorkflowService(issueSvc)
	feedbackSvc := service.NewFeedbackService(feedbackRepo, bus)

	srv := &Server{
		Config:           cfg,
		DB:               db,
		EventBus:         bus,
		ProjectService:   projectSvc,
		AgentService:     agentSvc,
		IssueService:     issueSvc,
		CommentService:   commentSvc,
		WorkflowService:  workflowSvc,
		FeedbackService:  feedbackSvc,
		Authenticator:    authn,
		NotifService:     notifSvc,
		MatchingEngine:   matchingEngine,
	}

	log.Println("[server] initialized")

	if err := SeedData(db); err != nil {
		log.Printf("[server] seed data: %v", err)
	}

	return srv, nil
}
