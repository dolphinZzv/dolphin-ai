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
	Authenticator    *auth.Authenticator
	NotifService     *notifications.Service
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

	// Init auth
	authn := auth.New(cfg.JWTSecret, cfg.BootstrapToken)

	// Init matching engine
	matchingEngine := matching.NewEngine(agentRepo, gormrepo.NewLabelRepo(db), assigneeRepo, issueRepo)
	matchingEngine.Subscribe(bus)

	// Init notification service
	notifSvc := notifications.NewService()
	notifSvc.Subscribe(bus)

	// Init services
	projectSvc := service.NewProjectService(projectRepo, memberRepo)
	agentSvc := service.NewAgentService(agentRepo, bus, authn)
	commentSvc := service.NewCommentService(commentRepo, timelineRepo, bus)
	issueSvc := service.NewIssueService(issueRepo, assigneeRepo, timelineRepo, projectRepo, bus)
	workflowSvc := service.NewWorkflowService(issueSvc)

	srv := &Server{
		Config:           cfg,
		DB:               db,
		EventBus:         bus,
		ProjectService:   projectSvc,
		AgentService:     agentSvc,
		IssueService:     issueSvc,
		CommentService:   commentSvc,
		WorkflowService:  workflowSvc,
		Authenticator:    authn,
		NotifService:     notifSvc,
	}

	log.Println("[server] initialized")

	return srv, nil
}
