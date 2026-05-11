package graph

import (
	"net/http"

	"chick/internal/service"

	gqlhandler "github.com/99designs/gqlgen/graphql/handler"
)

func NewHandler(
	projectSvc *service.ProjectService,
	agentSvc *service.AgentService,
	issueSvc *service.IssueService,
	commentSvc *service.CommentService,
	workflowSvc *service.WorkflowService,
) http.Handler {
	resolver := NewResolver(projectSvc, agentSvc, issueSvc, commentSvc, workflowSvc)
	cfg := Config{Resolvers: resolver}
	srv := gqlhandler.NewDefaultServer(NewExecutableSchema(cfg))
	return srv
}
