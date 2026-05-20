package graph

import (
	"net/http"

	"chick/internal/events"
	"chick/internal/notifications"
	"chick/internal/service"

	gqlhandler "github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
)

func NewHandler(
	projectSvc *service.ProjectService,
	agentSvc *service.AgentService,
	issueSvc *service.IssueService,
	commentSvc *service.CommentService,
	proposalSvc *service.ProposalService,
	taskSvc *service.TaskService,
	workflowSvc *service.WorkflowService,
	feedbackSvc *service.FeedbackService,
	notifSvc *notifications.Service,
	eventBus *events.Bus,
	allowHumanRegistration bool,
) http.Handler {
	resolver := NewResolver(projectSvc, agentSvc, issueSvc, commentSvc, proposalSvc, taskSvc, workflowSvc, feedbackSvc, notifSvc, eventBus, allowHumanRegistration)
	cfg := Config{Resolvers: resolver}
	srv := gqlhandler.NewDefaultServer(NewExecutableSchema(cfg))
	srv.Use(extension.FixedComplexityLimit(1000))
	return srv
}
