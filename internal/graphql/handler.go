package graph

import (
	"net/http"

	"chick/internal/events"
	"chick/internal/service"

	gqlhandler "github.com/99designs/gqlgen/graphql/handler"
)

func NewHandler(
	projectSvc *service.ProjectService,
	agentSvc *service.AgentService,
	issueSvc *service.IssueService,
	commentSvc *service.CommentService,
	workflowSvc *service.WorkflowService,
	feedbackSvc *service.FeedbackService,
	eventBus *events.Bus,
) http.Handler {
	resolver := NewResolver(projectSvc, agentSvc, issueSvc, commentSvc, workflowSvc, feedbackSvc, eventBus)
	cfg := Config{Resolvers: resolver}
	srv := gqlhandler.NewDefaultServer(NewExecutableSchema(cfg))
	return srv
}
