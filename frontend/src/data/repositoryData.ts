import type {
  GradingResponse,
  RecommendationsResponse,
  SourceCraftRepository,
} from "../api/contracts"
import { HttpRepoHealthApi, type RepoHealthApi } from "../api/client"
import { repositoryFromSourceCraft } from "../api/mapper"
import {
  DEFAULT_REPOSITORY_ID,
  DEMO_RECOMMENDATIONS,
  DEMO_REPOSITORIES,
} from "../mocks/demoData"
import type { RepositorySummary } from "../types"

export interface RepositoryDataSource {
  listRepositories(): Promise<RepositorySummary[]>
  getGrading(
    repository: RepositorySummary,
    userToken?: string,
  ): Promise<GradingResponse>
  getRecommendations(
    repository: RepositorySummary,
    userToken?: string,
  ): Promise<RecommendationsResponse>
}

function gradingFromSummary(repository: RepositorySummary): GradingResponse {
  return {
    status: repository.gradeStatus ?? "FINISHED",
    repo_slug: repository.repoSlug ?? repository.id,
    health_score: repository.healthScore,
    category_scores: Object.fromEntries(
      Object.entries(repository.categoryScores).map(([metricId, metric]) => [
        metricId,
        metric.score,
      ]),
    ),
    missing_data: repository.missingData ?? [],
    graded_at: repository.gradedAt,
  }
}

function withRepositoryIdentity(
  repository: RepositorySummary,
): RepositorySummary {
  const [orgSlug, repoSlug] = repository.fullName.split("/")
  return {
    ...repository,
    orgSlug: repository.orgSlug ?? orgSlug,
    repoSlug: repository.repoSlug ?? repoSlug ?? repository.id,
    defaultBranch: repository.defaultBranch ?? "main",
    visibility: repository.visibility ?? "public",
    gradeStatus: repository.gradeStatus ?? "FINISHED",
    missingData: repository.missingData ?? [],
  }
}

export class MockRepositoryDataSource implements RepositoryDataSource {
  async listRepositories(): Promise<RepositorySummary[]> {
    return DEMO_REPOSITORIES.map(withRepositoryIdentity)
  }

  async getGrading(repository: RepositorySummary): Promise<GradingResponse> {
    return gradingFromSummary(repository)
  }

  async getRecommendations(
    repository: RepositorySummary,
  ): Promise<RecommendationsResponse> {
    return {
      status: "FINISHED",
      reccomendations:
        repository.id === DEFAULT_REPOSITORY_ID
          ? DEMO_RECOMMENDATIONS.map((item) => ({
              title: item.title,
              description: item.description,
              priority: item.priority,
              current_score: item.currentScore,
              projected_score: item.projectedScore,
            }))
          : [],
    }
  }
}

export class HttpRepositoryDataSource implements RepositoryDataSource {
  private readonly api: RepoHealthApi
  private readonly catalog: RepositorySummary[]

  constructor(catalog: RepositorySummary[], api = new HttpRepoHealthApi()) {
    this.catalog = catalog
    this.api = api
  }

  async listRepositories(): Promise<RepositorySummary[]> {
    return this.catalog.map(withRepositoryIdentity)
  }

  getGrading(
    repository: RepositorySummary,
    userToken?: string,
  ): Promise<GradingResponse> {
    const { orgSlug, repoSlug } = this.getSlugs(repository)
    return this.api.getGrading(orgSlug, repoSlug, userToken)
  }

  getRecommendations(
    repository: RepositorySummary,
    userToken?: string,
  ): Promise<RecommendationsResponse> {
    const { orgSlug, repoSlug } = this.getSlugs(repository)
    return this.api.getRecommendations(orgSlug, repoSlug, userToken)
  }

  private getSlugs(repository: RepositorySummary): {
    orgSlug: string
    repoSlug: string
  } {
    const [fullNameOrg, fullNameRepo] = repository.fullName.split("/")
    const orgSlug = repository.orgSlug ?? fullNameOrg
    const repoSlug = repository.repoSlug ?? fullNameRepo ?? repository.id
    if (!orgSlug || !repoSlug)
      throw new Error("Repository is missing org_slug or repo_slug")
    return { orgSlug, repoSlug }
  }
}

export function repositorySummaryFromSourceCraft(
  repository: SourceCraftRepository,
  grading?: GradingResponse,
): RepositorySummary {
  return repositoryFromSourceCraft(repository, grading)
}

export const mockRepositoryDataSource = new MockRepositoryDataSource()

export function createRepositoryDataSource(): RepositoryDataSource {
  if (import.meta.env.VITE_DATA_SOURCE === "http") {
   return new HttpRepositoryDataSource(DEMO_REPOSITORIES)
  }
  return mockRepositoryDataSource
}
