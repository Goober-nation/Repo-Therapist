/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Public Yandex ID OAuth application ID (never the client secret). */
  readonly VITE_YANDEX_CLIENT_ID?: string
  /** Public site origin for badge links, e.g. https://example.org (defaults to the page origin). */
  readonly VITE_PUBLIC_URL?: string
}
