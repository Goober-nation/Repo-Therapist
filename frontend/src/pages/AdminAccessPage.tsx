import { useCallback, useState } from "react"
import type { HttpRepoHealthApi } from "../api/client"
import type { AdminMeResponse } from "../api/contracts"
import AuditLog from "../components/AuditLog"
import CatalogPanel from "../components/CatalogPanel"
import MetricsPanel from "../components/MetricsPanel"
import GradingPanel from "../components/GradingPanel"
import SettingsPanel from "../components/SettingsPanel"
import { Button, Card, PageHeading } from "../components/Shell"

/**
 * The "Доступ админа" tab: enter the admin password to give the signed-in account admin rights for the
 * rest of its session, and use the admin tools right here once they are given. `status` is undefined
 * while it is not known: `error` then says why it could not be loaded. The password is kept only in
 * this form while it is typed, sent in a request body, and cleared as soon as it has been sent.
 */
export default function AdminAccessPage({
  api,
  status,
  error,
  onRetry,
  onStatusChange,
}: {
  api: HttpRepoHealthApi
  status: AdminMeResponse | undefined
  error?: string
  onRetry: () => void
  onStatusChange: (status: AdminMeResponse) => void
}) {
  return (
    <div className="page-stack">
      <PageHeading
        eyebrow="Администрирование"
        title="Доступ админа"
        description={
          status?.is_admin
            ? "У вашей учётной записи есть права администратора: инструменты ниже доступны до конца этого входа."
            : "Введите пароль администратора, и в этой вкладке откроются инструменты администратора до конца этого входа."
        }
      />

      {status === undefined && !error && (
        <Card className="empty-state">
          <p role="status">Проверяем доступ…</p>
        </Card>
      )}

      {status === undefined && error && (
        <Card className="empty-state">
          <div role="alert">
            <strong>Не удалось проверить доступ</strong>
            <p>{error}</p>
          </div>
          <Button onClick={onRetry}>Повторить</Button>
        </Card>
      )}

      {status !== undefined && status.is_admin && <AdminTools api={api} status={status} onStatusChange={onStatusChange} />}

      {status !== undefined && !status.is_admin && !status.unlock_available && (
        <Card className="empty-state">
          <strong>Вход по паролю не настроен</strong>
          <p>На этом стенде не задан пароль администратора.</p>
        </Card>
      )}

      {status !== undefined && !status.is_admin && status.unlock_available && !status.signed_in && (
        <Card className="empty-state">
          <strong>Сначала войдите через Яндекс</strong>
          <p>Права администратора привязываются к вашей учётной записи, поэтому нужен вход через Яндекс ID. Если вы уже входили, сессия могла закончиться, войдите снова.</p>
        </Card>
      )}

      {status !== undefined && !status.is_admin && status.unlock_available && status.signed_in && (
        <PasswordForm api={api} onStatusChange={onStatusChange} />
      )}
    </div>
  )
}

function PasswordForm({
  api,
  onStatusChange,
}: {
  api: HttpRepoHealthApi
  onStatusChange: (status: AdminMeResponse) => void
}) {
  const [password, setPassword] = useState("")
  const [isBusy, setIsBusy] = useState(false)
  const [error, setError] = useState<string>()

  const submit = (event: React.FormEvent) => {
    event.preventDefault()
    if (!password || isBusy) return

    const sent = password
    setPassword("")
    setIsBusy(true)
    setError(undefined)

    api
      .unlockAdmin(sent)
      .then(onStatusChange)
      .catch((failure: unknown) => setError(failure instanceof Error ? failure.message : "Не удалось проверить пароль."))
      .finally(() => setIsBusy(false))
  }

  return (
    <Card>
      <form className="access-form" onSubmit={submit} autoComplete="off">
        <label className="access-form__field">
          <span>Пароль администратора</span>
          <input
            type="password"
            name="admin-password"
            autoComplete="off"
            value={password}
            maxLength={256}
            disabled={isBusy}
            onChange={(event) => setPassword(event.target.value)}
          />
        </label>
        {error && (
          <p className="access-form__error" role="alert">
            {error}
          </p>
        )}
        <Button variant="primary" type="submit" disabled={!password || isBusy}>
          {isBusy ? "Проверяем…" : "Получить доступ"}
        </Button>
        <p className="muted">
          Доступ действует до конца этого входа. Каждая попытка записывается в журнал; после 5 неверных паролей вход
          блокируется на 15 минут.
        </p>
      </form>
    </Card>
  )
}

/** What an admin sees: how the rights were given, the way to take them off, and the admin tools. */
function AdminTools({
  api,
  status,
  onStatusChange,
}: {
  api: HttpRepoHealthApi
  status: AdminMeResponse
  onStatusChange: (status: AdminMeResponse) => void
}) {
  const [isBusy, setIsBusy] = useState(false)
  const [error, setError] = useState<string>()
  const [auditReload, setAuditReload] = useState(0)
  const reloadAudit = useCallback(() => setAuditReload((value) => value + 1), [])

  const lock = () => {
    setIsBusy(true)
    setError(undefined)

    api
      .lockAdmin()
      .then(onStatusChange)
      .catch((failure: unknown) => setError(failure instanceof Error ? failure.message : "Не удалось снять права."))
      .finally(() => setIsBusy(false))
  }

  return (
    <>
      <Card className="access-status">
        <div>
          <strong>Вы администратор</strong>
          <p className="muted">
            {status.via_password
              ? "Права получены по паролю и действуют до конца этого входа."
              : "Вы указаны в списке администраторов."}
          </p>
          {error && (
            <p className="access-form__error" role="alert">
              {error}
            </p>
          )}
        </div>
        {status.via_password && (
          <Button onClick={lock} disabled={isBusy}>
            {isBusy ? "Снимаем…" : "Снять права"}
          </Button>
        )}
      </Card>

      <CatalogPanel api={api} onChanged={reloadAudit} />

      <GradingPanel api={api} onChanged={reloadAudit} />

      <SettingsPanel api={api} onChanged={reloadAudit} />

      <MetricsPanel api={api} />

      <AuditLog api={api} scope="admin" reloadKey={auditReload} />

      <AuditLog api={api} scope="users" />
    </>
  )
}
