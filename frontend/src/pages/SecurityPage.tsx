import {
  Badge,
  Button,
  Card,
  Divider,
  Label,
  PageHeading,
  ScoreBar,
  SectionHeading,
} from "../components/Shell"
import {
  DEMO_SECURITY_BREAKDOWN,
  DEMO_SECURITY_FINDINGS,
  DEFAULT_REPOSITORY_ID,
  getHealthLabel,
} from "../mocks/demoData"
import type { RepositorySummary } from "../types"

function securityTone(score: number): "good" | "warning" | "critical" {
  if (score >= 85) return "good"
  if (score >= 60) return "warning"
  return "critical"
}

export default function SecurityPage({
  repository,
  onBack,
}: {
  repository: RepositorySummary
  onBack: () => void
}) {
  const hasSecuritySample = repository.id === DEFAULT_REPOSITORY_ID
  const securityScore = repository.categoryScores.security.score

  return (
    <div className="page-stack">
      <PageHeading
        eyebrow={repository.fullName}
        title="Безопасность"
        description="Оценка безопасности и результаты сканирования репозитория."
        actions={
          <Button variant="secondary" onClick={onBack}>
            ← К обзору
          </Button>
        }
      />

      <div className="security-summary-grid">
        <Card className="security-score-card">
          <Label>Оценка безопасности</Label>
          <div className="health-score">
            {securityScore}
            <span>/100</span>
          </div>
          <Badge tone={securityTone(securityScore)}>
            {getHealthLabel(securityScore)}
          </Badge>
          <ScoreBar
            score={securityScore}
            label={`Безопасность: ${securityScore} из 100`}
          />
          <p className="muted-copy">
            Безопасность — одна из категорий Repo Health Score.
          </p>
        </Card>
        <Card className="security-impact-card">
          <Label>Возможное влияние</Label>
          {hasSecuritySample ? (
            <>
              <h2>
                82 <span>→</span> 89
              </h2>
              <p>Расчёт после устранения критических уязвимостей.</p>
              <Badge tone="good">До +7 баллов</Badge>
            </>
          ) : (
            <>
              <h2>—</h2>
              <p>
                Возможное изменение score для этого проекта пока не рассчитано.
              </p>
              <Badge>Нет данных</Badge>
            </>
          )}
        </Card>
      </div>

      {hasSecuritySample ? (
        <>
          <Card>
            <SectionHeading title="Разбивка по показателям" />
            <div className="security-breakdown">
              {DEMO_SECURITY_BREAKDOWN.map((metric, index) => (
                <div className="security-breakdown__row" key={metric.label}>
                  {index > 0 && <Divider />}
                  <span>{metric.label}</span>
                  <ScoreBar
                    score={metric.score}
                    label={`${metric.label}: ${metric.score} из 100`}
                  />
                  <strong>
                    {metric.score}
                    <small>/100</small>
                  </strong>
                </div>
              ))}
            </div>
          </Card>

          <Card className="findings-card">
            <SectionHeading
              title="Обнаруженные уязвимости"
              description="Найденные проблемы, отсортированные по критичности."
            />
            <div className="table-scroll">
              <div
                className="findings-table"
                role="table"
                aria-label="Список уязвимостей"
              >
                <div className="findings-row findings-row--header" role="row">
                  <span role="columnheader">Критичность</span>
                  <span role="columnheader">Пакет</span>
                  <span role="columnheader">CVE</span>
                  <span role="columnheader">Установленная версия</span>
                  <span role="columnheader">Исправление</span>
                </div>
                {DEMO_SECURITY_FINDINGS.map((finding) => (
                  <div className="findings-row" role="row" key={finding.id}>
                    <span role="cell">
                      <Badge
                        tone={
                          finding.severity === "critical"
                            ? "critical"
                            : "warning"
                        }
                      >
                        {finding.severity === "critical"
                          ? "Критично"
                          : "Высокая"}
                      </Badge>
                    </span>
                    <strong role="cell">{finding.packageName}</strong>
                    <span className="mono" role="cell">
                      {finding.cve}
                    </span>
                    <span role="cell">{finding.installedVersion}</span>
                    <span role="cell">
                      {finding.fixAvailable ? "Доступно" : "Нет"}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          </Card>

          <Card className="security-recommendation">
            <Label>Рекомендуемое действие</Label>
            <h2>Обновить уязвимые зависимости</h2>
            <p>
              Проверьте совместимость обновлений lodash, requests и axios, затем
              пересоберите проект и повторно запустите проверку. Это может
              поднять Security Score с 64 до 91 после повторного сканирования.
            </p>
            <Button variant="primary" onClick={onBack}>
              Вернуться к обзору
            </Button>
          </Card>
        </>
      ) : (
        <Card className="empty-state">
          <strong>Подробности AppSec пока недоступны для этого проекта.</strong>
          <p>
            Повторите проверку позже, чтобы получить детализацию сканирования.
          </p>
        </Card>
      )}
      <p className="data-caption">
        В рабочей версии доступ к данным AppSec должен проверяться сервером.
      </p>
    </div>
  )
}
