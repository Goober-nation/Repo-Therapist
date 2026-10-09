FROM node:22-alpine AS build
WORKDIR /src
RUN corepack enable
ARG VITE_YANDEX_CLIENT_ID=
ENV VITE_YANDEX_CLIENT_ID=$VITE_YANDEX_CLIENT_ID
ARG VITE_PUBLIC_URL=
ENV VITE_PUBLIC_URL=$VITE_PUBLIC_URL
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY . .
RUN pnpm build

FROM nginx:1.29-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/dist /usr/share/nginx/html
