import type { SettingResponse, SettingSource } from "./api/contracts";
import type { Tone } from "./metrics";

const GROUP_LABELS: Record<string, string> = {
  rate_limits: "Лимиты запросов",
  grading: "Оценка и переоценка",
  llm: "LLM",
};

export function groupLabel(group: string): string {
  return Object.hasOwn(GROUP_LABELS, group) ? GROUP_LABELS[group] : group;
}

const SOURCE_LABELS: Record<SettingSource, { label: string; tone: Tone }> = {
  env: { label: "Окружение", tone: "warning" },
  override: { label: "Изменено в панели", tone: "good" },
  file: { label: "app.yaml", tone: "neutral" },
  default: { label: "По умолчанию в коде", tone: "neutral" },
};

/** Where the running value comes from, as a badge. */
export function sourceLabel(source: SettingSource): {
  label: string;
  tone: Tone;
} {
  return SOURCE_LABELS[source] ?? { label: source, tone: "neutral" };
}

/** The value the panel can name: the override, else the app.yaml value; null for the code default. */
export function shownValue(
  setting: Pick<SettingResponse, "override" | "file_value">,
): string | null {
  return setting.override ?? setting.file_value;
}

/** Russian wording of a value for reading: booleans as да/нет, durations as typed (90s, 10m, 2h). */
export function formatValue(
  setting: Pick<SettingResponse, "type">,
  value: string | null,
): string {
  if (value === null || value === "") return "по умолчанию в коде";
  if (setting.type === "bool")
    return value === "true" ? "да" : value === "false" ? "нет" : value;
  return value;
}

/** Allowed range of a key, e.g. "от 1 до 100000" or "от 1s до 24h"; "" without bounds. */
export function boundsLabel(
  setting: Pick<SettingResponse, "min" | "max">,
): string {
  if (setting.min && setting.max) return `от ${setting.min} до ${setting.max}`;
  if (setting.min) return `не меньше ${setting.min}`;
  if (setting.max) return `не больше ${setting.max}`;
  return "";
}

const DURATION = /^(\d+(\.\d+)?(ns|us|µs|ms|s|m|h))+$/;
const UNIT_SECONDS: Record<string, number> = {
  ns: 1e-9,
  us: 1e-6,
  µs: 1e-6,
  ms: 1e-3,
  s: 1,
  m: 60,
  h: 3600,
};

/** Seconds in a Go duration ("1h30m"), or NaN when it is not one. */
export function durationSeconds(value: string): number {
  const text = value.trim();
  if (text === "0") return 0;
  if (!DURATION.test(text)) return Number.NaN;
  let total = 0;
  for (const [, amount, , unit] of text.matchAll(
    /(\d+(\.\d+)?)(ns|us|µs|ms|s|m|h)/g,
  ))
    total += Number(amount) * UNIT_SECONDS[unit];
  return total;
}

function numeric(type: SettingResponse["type"], value: string): number {
  const text = value.trim();
  if (type === "int") return /^-?\d+$/.test(text) ? Number(text) : Number.NaN;
  if (type === "float")
    return text !== "" && Number.isFinite(Number(text))
      ? Number(text)
      : Number.NaN;
  return durationSeconds(text);
}

/**
 * The same check the Aggregator runs, so a typo is caught before the request; the Aggregator still
 * decides. Returns the Russian problem, or "" when the value fits.
 */
export function validateSetting(
  setting: Pick<SettingResponse, "type" | "min" | "max" | "values">,
  value: string,
): string {
  const text = value.trim();
  if (text === "") return "Введите значение.";
  switch (setting.type) {
    case "bool":
      return text === "true" || text === "false" ? "" : "Выберите да или нет.";
    case "enum":
      return setting.values?.includes(text)
        ? ""
        : `Допустимо: ${setting.values?.join(", ") ?? ""}.`;
    case "string":
      return "";
  }
  const n = numeric(setting.type, text);
  if (Number.isNaN(n)) {
    return setting.type === "duration"
      ? "Укажите длительность, например 90s, 10m или 2h."
      : setting.type === "int"
        ? "Нужно целое число."
        : "Нужно число.";
  }
  if (setting.min && n < numeric(setting.type, setting.min))
    return `Меньше допустимого (${boundsLabel(setting)}).`;
  if (setting.max && n > numeric(setting.type, setting.max))
    return `Больше допустимого (${boundsLabel(setting)}).`;
  return "";
}

/** Settings grouped in their order of appearance. */
export function groupSettings(
  settings: SettingResponse[],
): { group: string; settings: SettingResponse[] }[] {
  const groups: { group: string; settings: SettingResponse[] }[] = [];
  for (const setting of settings) {
    let entry = groups.find((g) => g.group === setting.group);
    if (!entry) {
      entry = { group: setting.group, settings: [] };
      groups.push(entry);
    }
    entry.settings.push(setting);
  }
  return groups;
}

/**
 * Panel layout: each group splits into topical sections, related keys (a limit and its window) side by
 * side, with a short label because the section heading already names the topic. A key not listed here
 * still shows, in a trailing "Прочее" section of its group, under its app.yaml title.
 */
const SECTIONS: {
  group: string;
  title: string;
  wide?: boolean;
  keys: [key: string, label: string][];
}[] = [
  {
    group: "rate_limits",
    title: "Весь сайт, с одного IP",
    keys: [
      ["gateway.rate_limit", "Запросов за окно"],
      ["gateway.rate_limit_window", "Окно"],
    ],
  },
  {
    group: "rate_limits",
    title: "Публичный API, с одного IP",
    keys: [
      ["gateway.public_api_rate_limit", "Запросов за окно"],
      ["gateway.public_api_rate_limit_window", "Окно"],
    ],
  },
  {
    group: "rate_limits",
    title: "Выгрузка оценок, с одного IP",
    keys: [
      ["gateway.export_rate_limit", "Запросов за окно"],
      ["gateway.export_rate_limit_window", "Окно"],
    ],
  },
  {
    group: "rate_limits",
    title: "Оценки по запросу, на весь сервис",
    wide: true,
    keys: [
      [
        "aggregator.grading_request_limit",
        "Новых оценок за окно (0 — без лимита)",
      ],
      ["aggregator.grading_request_window", "Окно"],
      [
        "gateway.force_regrade_cooldown_minutes",
        "Пауза между принудительными переоценками, мин (0 — без паузы)",
      ],
    ],
  },
  {
    group: "grading",
    title: "Плановая переоценка",
    keys: [
      ["aggregator.grading_regrade_interval", "Период"],
      [
        "aggregator.grading_regrade_limit",
        "Топ-N публичных репозиториев (0 — все)",
      ],
    ],
  },
  {
    group: "grading",
    title: "Пакетная оценка",
    keys: [
      [
        "aggregator.batch_grading_max_limit",
        "Наибольший размер пакета (0 — без ограничения)",
      ],
      ["aggregator.batch_grading_item_timeout", "Лимит на один репозиторий"],
    ],
  },
  {
    group: "grading",
    title: "Время на оценку",

    keys: [
      ["shared.clone_timeout", "Клонирование репозитория"],
      ["shared.grade_timeout", "Расчёт оценки"],
      ["aggregator.grading_lock_timeout", "Снятие зависшей блокировки"],
      ["grade.grade_job_ttl", "Хранение токена задачи"],
    ],
  },
  {
    group: "llm",
    title: "Оценка документации",
    keys: [
      ["grade.llm_enabled", "Запрашивать у LLM"],
      ["llm.llm_docs_mode", "Режим (влияет на оценку)"],
    ],
  },
  {
    group: "llm",
    title: "Ожидание ответа",
    keys: [
      ["grade.llm_timeout", "Ожидание оценки документации"],
      ["llm.llm_docs_request_max_age_seconds", "Наибольший возраст запроса, с"],
    ],
  },
  {
    group: "llm",
    title: "Рекомендации",
    keys: [
      ["llm.llm_priority_threshold", "Граница приоритета MEDIUM/LOW"],
      ["aggregator.llm_request_ttl", "Подавление повторных запросов"],
    ],
  },
];

export interface SettingsSection {
  title: string;
  /** Spans the whole row, its keys side by side. */
  wide: boolean;
  items: { setting: SettingResponse; label: string }[];
}

/** Groups (in order of appearance) split into the sections above; empty sections are dropped. */
export function layoutSettings(
  settings: SettingResponse[],
): { group: string; sections: SettingsSection[] }[] {
  const byKey = new Map(settings.map((s) => [s.key, s]));
  const placed = new Set<string>();
  return groupSettings(settings).map(({ group, settings: own }) => {
    const sections: SettingsSection[] = [];
    for (const section of SECTIONS.filter((s) => s.group === group)) {
      const items = section.keys.flatMap(([key, label]) => {
        const setting = byKey.get(key);
        if (!setting || setting.group !== group) return [];
        placed.add(key);
        return [{ setting, label }];
      });
      if (items.length > 0)
        sections.push({
          title: section.title,
          wide: section.wide ?? false,
          items,
        });
    }
    const rest = own
      .filter((s) => !placed.has(s.key))
      .map((setting) => ({ setting, label: setting.title }));
    if (rest.length > 0)
      sections.push({ title: "Прочее", wide: true, items: rest });
    return { group, sections };
  });
}
