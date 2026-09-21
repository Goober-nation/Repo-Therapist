package domain

type RepoRepository interface {
	GetRepoBySlugs(orgSlug, repoSlug string) Repo
}

type GradesRepository interface {
}

type LLMRepository interface {
}
