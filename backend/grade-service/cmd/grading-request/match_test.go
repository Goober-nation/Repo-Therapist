package main

import "testing"

func TestMatchRepoReady(t *testing.T) {
	cases := []struct {
		name, value string
		want        bool
	}{
		{"this repo", `{"org_slug":"o","repo_slug":"r","sourcecraft_uuid":"u1","status":"ready"}`, true},
		{"this repo, failed clone", `{"org_slug":"o","repo_slug":"r","status":"failed","error":"boom"}`, true},
		{"other repo in same org", `{"org_slug":"o","repo_slug":"other","status":"ready"}`, false},
		{"same repo name, other org", `{"org_slug":"x","repo_slug":"r","status":"ready"}`, false},
		{"garbage", `not json`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := matchRepoReady([]byte(tc.value), "o", "r"); ok != tc.want {
				t.Errorf("match = %v, want %v", ok, tc.want)
			}
		})
	}
}

func TestMatchResult(t *testing.T) {
	cases := []struct {
		name, value string
		want        bool
	}{
		{"this repo and view", `{"sourcecraft_uuid":"u1","grade_mode":"public","score":80}`, true},
		{"this repo, failure result", `{"sourcecraft_uuid":"u1","grade_mode":"public","error":"GRADING_FAILED"}`, true},
		// The bug this fixes: a late result for an earlier request must not be taken as ours.
		{"late result of another repo", `{"sourcecraft_uuid":"u0","grade_mode":"public","score":85}`, false},
		{"same repo, other view", `{"sourcecraft_uuid":"u1","grade_mode":"private","score":70}`, false},
		{"garbage", `{`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := matchResult([]byte(tc.value), "u1", "public"); ok != tc.want {
				t.Errorf("match = %v, want %v", ok, tc.want)
			}
		})
	}
}
