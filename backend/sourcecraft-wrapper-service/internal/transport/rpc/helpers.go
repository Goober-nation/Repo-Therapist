package rpc

import (
	"context"
	"errors"
	"time"

	"sourcecraft-wrapper-service/internal/domain"
	pb "sourcecraft-wrapper-service/internal/transport/gen"

	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func mapRuns(runs []domain.Run) []*pb.Run {
	result := make([]*pb.Run, 0, len(runs))

	for _, run := range runs {
		result = append(result, mapRun(run))
	}

	return result
}
func mapRepositories(repos []domain.Repository) []*pb.Repository {
	result := make([]*pb.Repository, 0, len(repos))

	for _, repo := range repos {
		var lastActivityAt string
		if repo.LastActivity != nil {
			lastActivityAt = repo.LastActivity.Format(time.RFC3339Nano)
		}

		reactionCounts := make([]*pb.ReactionCount, 0, len(repo.Rating.ReactionCounts))

		for _, reaction := range repo.Rating.ReactionCounts {
			reactionCounts = append(
				reactionCounts,
				&pb.ReactionCount{
					Type:  reaction.Type,
					Count: reaction.Count,
				},
			)
		}

		result = append(result, &pb.Repository{
			Id:   repo.ID,
			Name: repo.Name,

			Organization: &pb.Organization{
				Id:   repo.Organization.ID,
				Slug: repo.Organization.Slug,
			},

			Slug:        repo.Slug,
			Description: repo.Description,

			Logo: &pb.Logo{
				Url: repo.Logo.URL,
			},

			WebUrl:         repo.WebURL,
			Visibility:     repo.Visibility,
			LastActivityAt: lastActivityAt,

			Rating: &pb.Rating{
				Value:          repo.Rating.Value,
				Percentile:     repo.Rating.Percentile,
				ReactionCounts: reactionCounts,
			},
		})
	}

	return result
}
func mapRun(run domain.Run) *pb.Run {
	return &pb.Run{
		Id:            run.ID,
		Slug:          run.Slug,
		Dates:         mapDates(run.Dates),
		Status:        run.Status,
		Workflows:     mapWorkflows(run.Workflows),
		EventType:     run.EventType,
		ErrorMessages: run.ErrorMessages,
		Pull:          mapPull(run.Pull),
		User:          mapRunUser(run.User),
	}
}

func mapWorkflows(workflows []domain.Workflow) []*pb.Workflow {
	result := make([]*pb.Workflow, 0, len(workflows))

	for _, workflow := range workflows {
		result = append(result, mapWorkflow(workflow))
	}

	return result
}

func mapWorkflow(workflow domain.Workflow) *pb.Workflow {
	return &pb.Workflow{
		Id:            workflow.ID,
		Slug:          workflow.Slug,
		Description:   workflow.Description,
		Dates:         mapDates(workflow.Dates),
		Status:        workflow.Status,
		Tasks:         mapTasks(workflow.Tasks),
		Progress:      mapProgress(workflow.Progress),
		EnvironmentId: workflow.EnvironmentID,
	}
}

func mapTasks(tasks []domain.Task) []*pb.Task {
	result := make([]*pb.Task, 0, len(tasks))

	for _, task := range tasks {
		result = append(result, mapTask(task))
	}

	return result
}

func mapTask(task domain.Task) *pb.Task {
	return &pb.Task{
		Id:          task.ID,
		Slug:        task.Slug,
		Description: task.Description,
		Dates:       mapDates(task.Dates),
		Status:      task.Status,
		Cubes:       mapCubes(task.Cubes),
		Progress:    mapProgress(task.Progress),
		Relations:   mapRelations(task.Relations),
	}
}

func mapCubes(cubes []domain.Cube) []*pb.Cube {
	result := make([]*pb.Cube, 0, len(cubes))

	for _, cube := range cubes {
		result = append(result, mapCube(cube))
	}

	return result
}

func mapCube(cube domain.Cube) *pb.Cube {
	return &pb.Cube{
		Id:        cube.ID,
		Slug:      cube.Slug,
		Dates:     mapDates(cube.Dates),
		Status:    cube.Status,
		Artifacts: mapArtifacts(cube.Artifacts),
		Relations: mapRelations(cube.Relations),
	}
}

func mapArtifacts(artifacts []*domain.Artifact) []*pb.Artifact {
	result := make([]*pb.Artifact, 0, len(artifacts))

	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}

		result = append(result, mapArtifact(*artifact))
	}

	return result
}

func mapArtifact(artifact domain.Artifact) *pb.Artifact {
	return &pb.Artifact{
		Id:          artifact.ID,
		LocalPath:   artifact.LocalPath,
		Dates:       mapDates(artifact.Dates),
		Status:      artifact.Status,
		DownloadUrl: artifact.DownloadURL,
	}
}

func mapProgress(progress domain.Progress) *pb.Progress {
	return &pb.Progress{
		Percent:     progress.Percent,
		CurrentCube: mapCubePtr(progress.CurrentCube),
	}
}

func mapCubePtr(cube *domain.Cube) *pb.Cube {
	if cube == nil {
		return nil
	}

	return mapCube(*cube)
}

func mapRelations(relations domain.Relations) *pb.Relations {
	return &pb.Relations{
		Needs: mapRelationsList(relations.Needs),
	}
}

func mapRelationsList(relations []domain.Relation) []*pb.Relation {
	result := make([]*pb.Relation, 0, len(relations))

	for _, relation := range relations {
		result = append(result, &pb.Relation{
			Id:   relation.ID,
			Slug: relation.Slug,
		})
	}

	return result
}

func mapDates(dates domain.Dates) *pb.Dates {
	return &pb.Dates{
		CreatedAt:  mapTimestampPtr(dates.CreatedAt),
		StartedAt:  mapTimestampPtr(dates.StartedAt),
		FinishedAt: mapTimestampPtr(dates.FinishedAt),
		UpdatedAt:  mapTimestampPtr(dates.UpdatedAt),
	}
}

func mapPull(pull *domain.Pull) *pb.Pull {
	if pull == nil {
		return nil
	}

	return &pb.Pull{
		Id:   pull.ID,
		Slug: pull.Slug,
	}
}

func mapRunUser(user *domain.RunUser) *pb.RunUser {
	if user == nil {
		return nil
	}

	return &pb.RunUser{
		Id:   user.ID,
		Slug: user.Slug,
	}
}

func mapIssues(issues []domain.Issue) []*pb.Issue {
	result := make([]*pb.Issue, 0, len(issues))

	for _, issue := range issues {
		result = append(result, mapIssue(issue))
	}

	return result
}

func mapIssue(issue domain.Issue) *pb.Issue {
	return &pb.Issue{
		Id:          issue.ID,
		Slug:        issue.Slug,
		Title:       issue.Title,
		Description: issue.Description,
		Status:      mapIssueStatus(issue.Status),
		Author:      mapIssueUser(issue.Author),
		UpdatedBy:   mapIssueUser(issue.UpdatedBy),
		CreatedAt:   mapTimestamp(issue.CreatedAt),
		UpdatedAt:   mapTimestamp(issue.UpdatedAt),
		Assignee:    mapIssueUserPtr(issue.Assignee),
		Labels:      mapLabels(issue.Labels),
		LinkedPrs:   mapLinkedPRs(issue.LinkedPRs),
		Priority:    issue.Priority,
		Visibility:  issue.Visibility,
		Milestone:   mapIssueMilestonePtr(issue.Milestone),
		Deadline:    mapTimestampPtr(issue.Deadline),
		StartedAt:   mapTimestampPtr(issue.StartedAt),
		CompletedAt: mapTimestampPtr(issue.CompletedAt),
		Repository:  mapRepositoryRef(issue.Repository),
	}
}

func mapIssueStatus(status domain.IssueStatus) *pb.IssueStatus {
	return &pb.IssueStatus{
		Id:         status.ID,
		Slug:       status.Slug,
		Name:       status.Name,
		StatusType: status.StatusType,
	}
}

func mapIssueUser(user domain.IssueUser) *pb.IssueUser {
	return &pb.IssueUser{
		Id:   user.ID,
		Slug: user.Slug,
	}
}

func mapIssueUserPtr(user *domain.IssueUser) *pb.IssueUser {
	if user == nil {
		return nil
	}

	return mapIssueUser(*user)
}

func mapLabels(labels []domain.Label) []*pb.Label {
	result := make([]*pb.Label, 0, len(labels))

	for _, label := range labels {
		result = append(result, &pb.Label{
			Id:    label.ID,
			Slug:  label.Slug,
			Name:  label.Name,
			Color: label.Color,
		})
	}

	return result
}

func mapLinkedPRs(prs []domain.LinkedPR) []*pb.LinkedPR {
	result := make([]*pb.LinkedPR, 0, len(prs))

	for _, pr := range prs {
		result = append(result, &pb.LinkedPR{
			Id:   pr.ID,
			Slug: pr.Slug,
		})
	}

	return result
}

func mapIssueMilestonePtr(milestone *domain.IssueMilestone) *pb.IssueMilestone {
	if milestone == nil {
		return nil
	}

	return &pb.IssueMilestone{
		Id:   milestone.ID,
		Slug: milestone.Slug,
	}
}

func mapRepositoryRef(repository domain.RepositoryRef) *pb.RepositoryRef {
	return &pb.RepositoryRef{
		Id:           repository.ID,
		Slug:         repository.Slug,
		Organization: mapOrganization(repository.Organization),
	}
}

func mapOrganization(organization domain.Organization) *pb.Organization {
	return &pb.Organization{
		Id:   organization.ID,
		Slug: organization.Slug,
	}
}

func mapMilestones(milestones []domain.Milestone) []*pb.Milestone {
	result := make([]*pb.Milestone, 0, len(milestones))

	for _, milestone := range milestones {
		result = append(result, mapMilestone(milestone))
	}

	return result
}

func mapMilestone(milestone domain.Milestone) *pb.Milestone {
	return &pb.Milestone{
		Id:          milestone.ID,
		Name:        milestone.Name,
		Slug:        milestone.Slug,
		Description: milestone.Description,
		StartDate:   mapTimestamp(milestone.StartDate),
		Deadline:    mapTimestamp(milestone.Deadline),
		Status:      milestone.Status,
		Author:      mapMilestoneUser(milestone.Author),
		UpdatedBy:   mapMilestoneUser(milestone.UpdatedBy),
		CreatedAt:   mapTimestamp(milestone.CreatedAt),
		UpdatedAt:   mapTimestamp(milestone.UpdatedAt),
	}
}

func mapMilestoneUser(user domain.MilestoneUser) *pb.MilestoneUser {
	return &pb.MilestoneUser{
		Id:   user.ID,
		Slug: user.Slug,
	}
}

func mapTimestamp(t time.Time) *timestamppb.Timestamp {
	return timestamppb.New(t)
}

func mapTimestampPtr(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}

	return timestamppb.New(*t)
}

func mapCloneURL(url domain.CloneURL) *pb.GetGitURLResponse {
	return &pb.GetGitURLResponse{
		HttpUrl: url.HttpURL,
		SshUrl:  url.SshURL,
	}
}

func tokenFromContext(ctx context.Context) (string, error) {
	var ErrMissingToken = errors.New("missing authorization token")
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", ErrMissingToken
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return "", ErrMissingToken
	}
	return values[0], nil
}
