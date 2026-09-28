import { defineConfig, loadEnv } from "vite"

import react from "@vitejs/plugin-react"

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "GATEWAY_")

  return {
    plugins: [react()],

    server: {
      proxy: {
        "/api": {
          target: env.GATEWAY_URL || "http://localhost:8080",
          changeOrigin: true,
          rewrite: (path) => path.replace(/^\/api(?=\/|$)/, ""),
        },
        // Same as the nginx location: the browser cannot call login.yandex.ru directly (CORS).
        "/yandex-profile/info": {
          target: "https://login.yandex.ru",
          changeOrigin: true,
          rewrite: (path) => path.replace(/^\/yandex-profile/, ""),
        },
      },
    },
  }
})
