import type {
  AttentionItem,
  MetricId,
  Recommendation,
  RepositorySummary,
  SecurityFinding,
} from "../types";

export const DEFAULT_REPOSITORY_ID = "awesome-project";

export const METRIC_LABELS: Record<MetricId, string> = {
  activity: "Активность",
  codeHealth: "Состояние кода",
  security: "Безопасность",
  ciCd: "CI/CD",
  documentation: "Документация",
  issues: "Issues",
};

export const DEMO_REPOSITORIES: RepositorySummary[] = [
  {
    id: "sourcecraft-sdk",
    name: "sourcecraft/sdk",
    fullName: "sourcecraft/sdk",
    language: "Go",
    healthScore: 96,
    trend: 2,
    likes: 482,
    lastActivityDaysAgo: 1,
    categoryScores: {
      activity: { score: 98, trend: 2 },
      codeHealth: { score: 95, trend: 1 },
      security: { score: 94, trend: 2 },
      ciCd: { score: 96, trend: 1 },
      documentation: { score: 90, trend: 0 },
      issues: { score: 94, trend: 1 },
    },
  },
  {
    id: "yandex-cloud-api",
    name: "yandex/cloud-api",
    fullName: "yandex/cloud-api",
    language: "Go",
    healthScore: 91,
    trend: 4,
    likes: 356,
    lastActivityDaysAgo: 2,
    categoryScores: {
      activity: { score: 94, trend: 3 },
      codeHealth: { score: 90, trend: 2 },
      security: { score: 89, trend: 1 },
      ciCd: { score: 93, trend: 2 },
      documentation: { score: 91, trend: 4 },
      issues: { score: 88, trend: 1 },
    },
  },
  {
    id: "ml-toolkit",
    name: "ml-toolkit",
    fullName: "team-labs/ml-toolkit",
    language: "Python",
    healthScore: 87,
    trend: 6,
    likes: 219,
    lastActivityDaysAgo: 1,
    categoryScores: {
      activity: { score: 90, trend: 6 },
      codeHealth: { score: 86, trend: 3 },
      security: { score: 82, trend: 2 },
      ciCd: { score: 88, trend: 4 },
      documentation: { score: 84, trend: 2 },
      issues: { score: 87, trend: 5 },
    },
  },
  {
    id: DEFAULT_REPOSITORY_ID,
    name: "awesome-project",
    fullName: "sourcecraft/awesome-project",
    language: "Python",
    healthScore: 82,
    trend: 4,
    likes: 128,
    lastActivityDaysAgo: 1,
    categoryScores: {
      activity: { score: 92, trend: 3 },
      codeHealth: { score: 78, trend: 1 },
      security: { score: 64, trend: -4 },
      ciCd: { score: 90, trend: 2 },
      documentation: { score: 85, trend: 0 },
      issues: { score: 74, trend: 1 },
    },
  },
  {
    id: "backend-core",
    name: "backend-core",
    fullName: "platform/backend-core",
    language: "Go",
    healthScore: 79,
    trend: 1,
    likes: 92,
    lastActivityDaysAgo: 3,
    categoryScores: {
      activity: { score: 84, trend: 1 },
      codeHealth: { score: 80, trend: 2 },
      security: { score: 69, trend: -1 },
      ciCd: { score: 82, trend: 2 },
      documentation: { score: 78, trend: 0 },
      issues: { score: 81, trend: 1 },
    },
  },
  {
    id: "data-platform",
    name: "data-platform",
    fullName: "analytics/data-platform",
    language: "Java",
    healthScore: 76,
    trend: -3,
    likes: 147,
    lastActivityDaysAgo: 6,
    categoryScores: {
      activity: { score: 81, trend: -2 },
      codeHealth: { score: 76, trend: -1 },
      security: { score: 73, trend: 1 },
      ciCd: { score: 75, trend: -4 },
      documentation: { score: 78, trend: 0 },
      issues: { score: 72, trend: -2 },
    },
  },
  {
    id: "frontend-kit",
    name: "frontend-kit",
    fullName: "web-tools/frontend-kit",
    language: "TypeScript",
    healthScore: 72,
    trend: -2,
    likes: 73,
    lastActivityDaysAgo: 9,
    categoryScores: {
      activity: { score: 70, trend: -3 },
      codeHealth: { score: 74, trend: 1 },
      security: { score: 76, trend: 0 },
      ciCd: { score: 68, trend: -2 },
      documentation: { score: 79, trend: 1 },
      issues: { score: 65, trend: -4 },
    },
  },
  {
    id: "old-library",
    name: "old-library",
    fullName: "archive/old-library",
    language: "C++",
    healthScore: 48,
    trend: -8,
    likes: 41,
    lastActivityDaysAgo: 62,
    categoryScores: {
      activity: { score: 42, trend: -8 },
      codeHealth: { score: 53, trend: -4 },
      security: { score: 51, trend: -3 },
      ciCd: { score: 45, trend: -6 },
      documentation: { score: 58, trend: -2 },
      issues: { score: 39, trend: -7 },
    },
  },
];

export const DEMO_ATTENTION_ITEMS: AttentionItem[] = [
  {
    id: "critical-dependencies",
    severity: "critical",
    category: "Безопасность",
    title: "3 критические уязвимости в зависимостях",
    description: "Проблемы обнаружены в двух пакетах.",
    impact: 7,
  },
  {
    id: "readme-outdated",
    severity: "warning",
    category: "Документация",
    title: "README не обновлялся шесть месяцев",
    description: "Актуальная инструкция поможет быстрее начать работу с проектом.",
    impact: 2,
  },
  {
    id: "slow-reviews",
    severity: "warning",
    category: "Issues",
    title: "Долгое время до первого ответа на задачу",
    description: "Среднее время ожидания ответа на задачу — 4,2 дня.",
    impact: 3,
  },
];

export const DEMO_RECOMMENDATIONS: Recommendation[] = [
  {
    id: "update-dependencies",
    priority: "high",
    title: "Обновить уязвимые зависимости",
    description: "Обновите пакеты до версий, в которых исправлены критические уязвимости.",
    currentScore: 82,
    projectedScore: 89,
  },
  {
    id: "add-contributing",
    priority: "medium",
    title: "Добавить CONTRIBUTING.md",
    description: "Опишите процесс разработки, тестирования и отправки изменений.",
    currentScore: 82,
    projectedScore: 85,
  },
  {
    id: "improve-issue-response",
    priority: "medium",
    title: "Сократить время ответа на issues",
    description: "Назначьте ответственных и настройте уведомления о новых задачах.",
    currentScore: 82,
    projectedScore: 86,
  },
];

export const DEMO_SECURITY_BREAKDOWN = [
  { label: "Уязвимости зависимостей", score: 45 },
  { label: "Поиск секретов", score: 100 },
  { label: "Актуальность зависимостей", score: 72 },
  { label: "Настройки безопасности", score: 68 },
];

export const DEMO_SECURITY_FINDINGS: SecurityFinding[] = [
  {
    id: "lodash-cve-2021-23337",
    severity: "critical",
    packageName: "lodash",
    cve: "CVE-2021-23337",
    installedVersion: "4.17.10",
    fixAvailable: true,
  },
  {
    id: "requests-cve-2022-40897",
    severity: "critical",
    packageName: "requests",
    cve: "CVE-2022-40897",
    installedVersion: "2.18",
    fixAvailable: true,
  },
  {
    id: "axios-cve-2021-3749",
    severity: "high",
    packageName: "axios",
    cve: "CVE-2021-3749",
    installedVersion: "0.21",
    fixAvailable: true,
  },
];

export const DEMO_SCORE_HISTORY = [
  { month: "Апр", score: 71 },
  { month: "Май", score: 73 },
  { month: "Июн", score: 76 },
  { month: "Июл", score: 75 },
  { month: "Авг", score: 78 },
  { month: "Сен", score: 82 },
];

export function getHealthLabel(score: number): string {
  if (score >= 90) return "Отличное состояние";
  if (score >= 75) return "Здоровый";
  if (score >= 60) return "Требует внимания";
  return "Под риском";
}

export function getLastActivityLabel(daysAgo: number): string {
  if (daysAgo === 0) return "Сегодня";
  if (daysAgo === 1) return "Вчера";
  if (daysAgo < 7) return `${daysAgo} дн. назад`;
  if (daysAgo < 30) return `${Math.floor(daysAgo / 7)} нед. назад`;
  return `${Math.floor(daysAgo / 30)} мес. назад`;
}
