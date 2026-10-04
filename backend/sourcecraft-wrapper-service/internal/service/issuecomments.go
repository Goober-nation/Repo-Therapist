package service

import (
	"context"
	"fmt"

	"sourcecraft-wrapper-service/internal/domain"
)

// GetIssueComments returns the comments (first page, up to 100) of one issue by its ID. Only the
// author and the time are kept: the comment text never leaves this service.
func (scw *SCWrapper) GetIssueComments(ctx context.Context, issueID string) ([]domain.IssueComment, error) {
	token, err := tokenFromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w:%w", domain.ErrUnauthenticated, err)
	}

	var resp struct {
		Comments []domain.IssueComment `json:"issue_comments"`
	}
	if err := scw.fetchJSON(ctx, token, fmt.Sprintf("/issues/id:%s/comments?page_size=100", issueID), &resp); err != nil {
		return nil, err
	}

	return resp.Comments, nil
}
