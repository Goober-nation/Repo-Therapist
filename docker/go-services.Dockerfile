# All Go services in one image; docker/go-entrypoint.sh starts them.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.work go.work.sum ./
COPY backend/ backend/
ENV CGO_ENABLED=0
# aggregator-service is omitted: it does not compile on main (domain.RepoGrade fields removed).
# Re-add: go build -o /out/aggregator-service ./backend/aggregator-service/cmd
RUN go build -o /out/sourcecraft-wrapper-service ./backend/sourcecraft-wrapper-service/cmd \
 && go build -o /out/security-wrapper-service ./backend/security-wrapper-service/cmd/security-wrapper-service \
 && go build -o /out/repository-service ./backend/repository-service/cmd/repository-service \
 && go build -o /out/grade-service ./backend/grade-service/cmd/grade-service \
 && go build -o /out/grading-request ./backend/grade-service/cmd/grading-request

FROM alpine:3.22
RUN apk add --no-cache bash git ca-certificates \
 && adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/ /usr/local/bin/
COPY config/ config/
COPY docker/go-entrypoint.sh /usr/local/bin/go-entrypoint.sh
USER app
ENTRYPOINT ["/usr/local/bin/go-entrypoint.sh"]
