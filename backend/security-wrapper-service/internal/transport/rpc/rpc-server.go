package rpc

import (
	"context"
	"errors"
	"log"
	"repo-therapist/security-wrapper-service/internal/domain"
	"repo-therapist/security-wrapper-service/internal/service"
	pb "repo-therapist/security-wrapper-service/internal/transport/gen"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ServerRPC struct {
	pb.UnimplementedSecurityWrapperServer
	service service.SecurityService
}

func NewServerRPC(serv service.SecurityService) *ServerRPC {
	return &ServerRPC{service: serv}
}

func (s *ServerRPC) GetAppsecResult(
	ctx context.Context,
	req *pb.GetAppsecResultRequest,
) (*pb.GetAppsecResultResponse, error) {
	result, err := s.service.GetAppsecResult(ctx, req.GetGitRepo())
	if err != nil {
		return nil, mapError(err)
	}
	return mapAppsecResult(result), nil
}

func mapError(err error) error {
	log.Println(err)
	switch {
	case errors.Is(err, domain.ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, "missing or invalid authorization token")
	case errors.Is(err, domain.ErrNoAccess):
		return status.Error(codes.PermissionDenied, "access denied")
	case errors.Is(err, domain.ErrReportFailed):
		return status.Error(codes.Internal, "report generation failed")
	case errors.Is(err, domain.ErrReportTimeout):
		return status.Error(codes.DeadlineExceeded, "timed out waiting for report generation")
	case errors.Is(err, domain.ErrExternalAPI):
		return status.Error(codes.Internal, "external API error")
	case errors.Is(err, domain.ErrInternalError):
		return status.Error(codes.Internal, "internal error")
	default:
		return status.Error(codes.Internal, "internal error")
	}
}
