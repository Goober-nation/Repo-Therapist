package rpc

import (
	"strings"
	"testing"
	"unicode/utf8"

	"sourcecraft-wrapper-service/internal/domain"
)

func TestRepoDescriptionIsCappedOnARuneBoundary(t *testing.T) {
	if got := repoDescription("short"); got != "short" {
		t.Errorf("short description changed: %q", got)
	}

	huge := strings.Repeat("ж", 8_000_000) // the size of the real 8.5 MB description, in two-byte runes
	got := repoDescription(huge)

	if utf8.RuneCountInString(got) != MaxRepoDescriptionRunes || !utf8.ValidString(got) {
		t.Errorf("capped to %d runes, valid=%v", utf8.RuneCountInString(got), utf8.ValidString(got))
	}

	repos := mapRepositories([]domain.Repository{{ID: "1", Description: huge}})
	if utf8.RuneCountInString(repos[0].GetDescription()) != MaxRepoDescriptionRunes {
		t.Error("mapRepositories must cap the description")
	}
}
