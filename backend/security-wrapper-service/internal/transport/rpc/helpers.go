package rpc

import (
	"repo-therapist/security-wrapper-service/internal/domain"
	pb "repo-therapist/security-wrapper-service/internal/transport/gen"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

func mapAppsecResult(result domain.AppsecResult) *pb.GetAppsecResultResponse {
	if !result.HasScan {
		return &pb.GetAppsecResultResponse{HasScan: false}
	}

	return &pb.GetAppsecResultResponse{
		HasScan:      true,
		Scan:         mapScanSummary(result.Scan),
		OpenCounts:   mapSeverityCounts(result.OpenCounts),
		DefectGroups: mapDefectGroups(result.DefectGroups),
	}
}

func mapScanSummary(scan domain.ScanSummary) *pb.ScanSummary {
	return &pb.ScanSummary{
		Uuid:              scan.UUID,
		Status:            string(scan.Status),
		ScanType:          scan.ScanType,
		CommitHash:        scan.CommitHash,
		TimeStarted:       mapTimestampPtr(scan.TimeStarted),
		TimeFinished:      mapTimestampPtr(scan.TimeFinished),
		TotalDefectGroups: scan.TotalDefectGroups,
		FoundDefectGroups: scan.FoundDefectGroups,
		OpenDefectGroups:  scan.OpenDefectGroups,
	}
}

func mapSeverityCounts(counts domain.SeverityCounts) *pb.SeverityCounts {
	return &pb.SeverityCounts{
		Critical: counts.Critical,
		High:     counts.High,
		Medium:   counts.Medium,
		Low:      counts.Low,
	}
}

func mapDefectGroups(summaries []domain.DefectGroupSummary) []*pb.DefectGroupSummary {
	result := make([]*pb.DefectGroupSummary, 0, len(summaries))
	for _, s := range summaries {
		result = append(result, &pb.DefectGroupSummary{
			RuleId:    s.RuleID,
			RuleName:  s.RuleName,
			Severity:  string(s.Severity),
			File:      s.File,
			StartLine: int32(s.StartLine),
			EndLine:   int32(s.EndLine),
			Message:   s.Message,
		})
	}
	return result
}

func mapTimestampPtr(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}
