package graph

import (
	"context"
	"errors"

	"chick/internal/auth"
	"chick/internal/models"
)

// requireAuth extracts the authenticated agent's ID from the context.
func requireAuth(ctx context.Context) (uint, error) {
	id, ok := auth.AgentIDFromContext(ctx)
	if !ok {
		return 0, errors.New("请先登录")
	}
	return id, nil
}

// requireProjectMember returns the agent ID if the caller is a member of the project.
func (r *Resolver) requireProjectMember(ctx context.Context, projectID uint) (uint, error) {
	agentID, err := requireAuth(ctx)
	if err != nil {
		return 0, err
	}
	role, err := r.ProjectSvc.GetMemberRole(projectID, agentID)
	if err != nil {
		return 0, errors.New("无权访问该项目")
	}
	_ = role
	return agentID, nil
}

// requireProjectOwner returns the agent ID if the caller is an owner of the project.
func (r *Resolver) requireProjectOwner(ctx context.Context, projectID uint) (uint, error) {
	agentID, err := requireAuth(ctx)
	if err != nil {
		return 0, err
	}
	role, err := r.ProjectSvc.GetMemberRole(projectID, agentID)
	if err != nil || role != models.ProjectRoleOwner {
		return 0, errors.New("需要项目 owner 权限")
	}
	return agentID, nil
}

// requireIssueProjectMember checks that the caller is a member of the project that owns the issue.
func (r *Resolver) requireIssueProjectMember(ctx context.Context, issueID uint) (uint, error) {
	if _, err := requireAuth(ctx); err != nil {
		return 0, err
	}
	issue, err := r.IssueSvc.GetByID(issueID)
	if err != nil {
		return 0, errors.New("issue not found")
	}
	return r.requireProjectMember(ctx, issue.ProjectID)
}

// requireIssueProjectMemberByProject checks project membership by project ID directly.
func (r *Resolver) requireIssueProjectMemberByProject(ctx context.Context, projectID uint) (uint, error) {
	return r.requireProjectMember(ctx, projectID)
}
