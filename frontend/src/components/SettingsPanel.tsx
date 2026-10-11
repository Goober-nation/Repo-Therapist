import { useCallback, useEffect, useState } from "react";
import type { HttpRepoHealthApi } from "../api/client";
import type { SettingResponse, SettingsResponse } from "../api/contracts";
import { formatDateTime } from "../metrics";
import {
  boundsLabel,
  formatValue,
  groupLabel,
  layoutSettings,
  shownValue,
  sourceLabel,
  validateSetting,
} from "../settingsView";
import { Badge, Button, Card, Label } from "./Shell";

/**
 * Admin overrides of config/app.yaml (docs/config.md). A change applies to the running services within
 * seconds and survives restarts; the environment always wins, so a key set there is read-only here.
 */
export default function SettingsPanel({
  api,
  onChanged,
}: {
  api: HttpRepoHealthApi;
  onChanged?: () => void;
}) {
  const [page, setPage] = useState<SettingsResponse>();
  const [loadError, setLoadError] = useState<string>();

  const load = useCallback(async () => {
    try {
      setPage(await api.getSettings());
      setLoadError(undefined);
    } catch (failure) {
      setLoadError(
        failure instanceof Error
          ? failure.message
          : "Не удалось загрузить настройки.",
      );
    }
  }, [api]);

  useEffect(() => {
    void load();
  }, [load]);

  const pending =
    page?.processes.filter((p) => p.restart_pending.length > 0) ?? [];

  return (
    <Card>
      <Label>Настройки</Label>
      <h2 className="regrade-title">Настройки сервисов</h2>
      <p className="muted">
        Значения из config/app.yaml можно изменить здесь: изменение доходит до
        сервисов за несколько секунд и сохраняется после перезапуска. Сброс
        возвращает значение из app.yaml. Настройка, заданная в переменных
        окружения, главнее и здесь не меняется.
      </p>

      {loadError && (
        <div className="empty-state" role="alert">
          <strong>Не удалось загрузить настройки</strong>
          <p>{loadError}</p>
          <Button onClick={() => void load()}>Повторить</Button>
        </div>
      )}

      {!page && !loadError && (
        <p className="muted" role="status">
          Загружаем…
        </p>
      )}

      {pending.map((p) => (
        <p key={p.name} className="settings-restart" role="status">
          <Badge tone="warning">Нужен перезапуск</Badge> {p.name}:{" "}
          {p.restart_pending.join(", ")}
        </p>
      ))}

      {page &&
        layoutSettings(page.settings).map(({ group, sections }) => (
          <section
            key={group}
            className="settings-group"
            aria-label={groupLabel(group)}
          >
            <h3>{groupLabel(group)}</h3>
            <div className="settings-grid">
              {sections.map((section) => (
                <div
                  key={section.title}
                  className={
                    section.wide
                      ? "settings-section settings-section--wide"
                      : "settings-section"
                  }
                >
                  <h4>{section.title}</h4>
                  <div className="settings-section__rows">
                    {section.items.map(({ setting, label }) => (
                      <SettingRow
                        key={setting.key}
                        api={api}
                        setting={setting}
                        label={label}
                        onSaved={(next) => {
                          setPage(
                            (current) =>
                              current && {
                                ...current,
                                settings: current.settings.map((s) =>
                                  s.key === next.key ? next : s,
                                ),
                              },
                          );
                          onChanged?.();
                        }}
                      />
                    ))}
                  </div>
                </div>
              ))}
            </div>
          </section>
        ))}
    </Card>
  );
}

function SettingRow({
  api,
  setting,
  label,
  onSaved,
}: {
  api: HttpRepoHealthApi;
  setting: SettingResponse;
  label: string;
  onSaved: (next: SettingResponse) => void;
}) {
  const current = shownValue(setting) ?? "";
  const [draft, setDraft] = useState(current);
  const [busy, setBusy] = useState<"save" | "reset">();
  const isBusy = busy !== undefined;
  const [error, setError] = useState<string>();
  const locked = setting.env_locked_by.length > 0;
  const source = sourceLabel(setting.effective_from);
  const problem =
    draft.trim() === current ? "" : validateSetting(setting, draft);
  const inputId = `setting-${setting.key}`;

  useEffect(() => setDraft(shownValue(setting) ?? ""), [setting]);

  const act = (
    kind: "save" | "reset",
    call: () => Promise<SettingResponse>,
  ) => {
    setBusy(kind);
    setError(undefined);
    call()
      .then(onSaved)
      .catch((failure: unknown) =>
        setError(
          failure instanceof Error ? failure.message : "Не удалось сохранить.",
        ),
      )
      .finally(() => setBusy(undefined));
  };

  return (
    <div className="settings-row">
      <div className="settings-row__head">
        <label htmlFor={inputId} title={setting.title}>
          {label}
        </label>
        {setting.effective_from !== "file" && (
          <Badge tone={source.tone}>{source.label}</Badge>
        )}
      </div>
      {locked ? (
        <p className="muted settings-row__note">
          Задано в окружении: {setting.env_locked_by.join(", ")}. Значение из
          окружения действует всегда.
        </p>
      ) : (
        <form
          className="settings-row__form"
          onSubmit={(event) => {
            event.preventDefault();
            if (!problem && draft.trim() !== current)
              act("save", () => api.setSetting(setting.key, draft.trim()));
          }}
        >
          <ValueInput
            id={inputId}
            setting={setting}
            value={draft}
            disabled={isBusy}
            onChange={setDraft}
          />
          <Button
            variant="primary"
            type="submit"
            disabled={isBusy || Boolean(problem) || draft.trim() === current}
          >
            {busy === "save" ? "Сохраняем…" : "Сохранить"}
          </Button>
          {setting.override !== null && (
            <Button
              disabled={isBusy}
              title="Вернуть значение из app.yaml"
              onClick={() => act("reset", () => api.resetSetting(setting.key))}
            >
              {busy === "reset" ? "Сбрасываем…" : "Сбросить"}
            </Button>
          )}
        </form>
      )}

      <small className="muted settings-row__note">
        {boundsLabel(setting) && `${boundsLabel(setting)} · `}app.yaml:{" "}
        {formatValue(setting, setting.file_value)}
        {setting.override !== null && (
          <>
            {" · "}в панели: {formatValue(setting, setting.override)}
            {setting.updated_at &&
              `, ${formatDateTime(setting.updated_at) ?? setting.updated_at}`}
          </>
        )}
      </small>
      <small className="settings-row__key">{setting.key}</small>
      {(problem || error) && (
        <p className="access-form__error" role="alert">
          {problem || error}
        </p>
      )}
    </div>
  );
}

function ValueInput({
  id,
  setting,
  value,
  disabled,
  onChange,
}: {
  id: string;
  setting: SettingResponse;
  value: string;
  disabled: boolean;
  onChange: (value: string) => void;
}) {
  if (setting.type === "bool" || setting.type === "enum") {
    const options =
      setting.type === "bool" ? ["true", "false"] : (setting.values ?? []);
    return (
      <select
        id={id}
        value={value}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
      >
        {value === "" && <option value="">по умолчанию в коде</option>}
        {options.map((option) => (
          <option key={option} value={option}>
            {formatValue(setting, option)}
          </option>
        ))}
      </select>
    );
  }

  return (
    <input
      id={id}
      value={value}
      disabled={disabled}
      inputMode={
        setting.type === "int" || setting.type === "float"
          ? "numeric"
          : undefined
      }
      placeholder={setting.type === "duration" ? "10m" : undefined}
      onChange={(event) => onChange(event.target.value)}
    />
  );
}
