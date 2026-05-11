package graph

import (
	"chick/internal/service"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require
// here.

type Resolver struct {
	ProjectSvc  *service.ProjectService
	AgentSvc    *service.AgentService
	IssueSvc    *service.IssueService
	CommentSvc  *service.CommentService
	WorkflowSvc *service.WorkflowService
}

func NewResolver(
	projectSvc *service.ProjectService,
	agentSvc *service.AgentService,
	issueSvc *service.IssueService,
	commentSvc *service.CommentService,
	workflowSvc *service.WorkflowService,
) *Resolver {
	return &Resolver{
		ProjectSvc:  projectSvc,
		AgentSvc:    agentSvc,
		IssueSvc:    issueSvc,
		CommentSvc:  commentSvc,
		WorkflowSvc: workflowSvc,
	}
}
