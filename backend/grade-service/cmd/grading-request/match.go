package main

import (
	"encoding/json"

	"repo-therapist/grade-service/internal/contracts"
)

// matchRepoReady reports whether a repo.ready message is the one for org/repo. ok=false means the
// message belongs to another request (or doesn't parse) and should be skipped.
func matchRepoReady(value []byte, org, repo string) (ready contracts.RepoReady, ok bool) {
	if err := json.Unmarshal(value, &ready); err != nil {
		return contracts.RepoReady{}, false
	}
	return ready, ready.OrgSlug == org && ready.RepoSlug == repo
}

// matchResult reports whether a grading.results message is the one for this repo's UUID and the
// requested grade mode — the public and private views of one repo are separate results.
func matchResult(value []byte, uuid, gradeMode string) (result contracts.GradingResultEvent, ok bool) {
	if err := json.Unmarshal(value, &result); err != nil {
		return contracts.GradingResultEvent{}, false
	}
	return result, result.SourcecraftUUID == uuid && result.GradeMode == gradeMode
}
