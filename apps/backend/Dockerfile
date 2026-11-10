# syntax=docker/dockerfile:1
# The backend embeds the web app, so this builds from the repository root:
#   docker build -f apps/backend/Dockerfile .
# Dockerfile.dockerignore next to this file limits the context to apps/web/ and apps/backend/.

FROM node:26-alpine AS web
WORKDIR /src
COPY apps/web/package.json apps/web/pnpm-lock.yaml apps/web/pnpm-workspace.yaml ./
# The pnpm version is pinned once, in package.json's packageManager field.
RUN npm install --global "pnpm@$(node -p "require('./package.json').packageManager.split('@')[1]")"
RUN --mount=type=cache,target=/root/.local/share/pnpm/store pnpm install --frozen-lockfile
COPY apps/web/ ./
ARG VERSION=dev
RUN pnpm build


FROM golang:1.27 AS build
WORKDIR /src
COPY apps/backend/go.mod apps/backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY apps/backend/ ./
COPY --from=web /src/build ./internal/web/dist
# The release, reported with every trace, metric, and log (release.yml passes the tag's version)
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/backend ./cmd/backend


FROM gcr.io/distroless/static-debian12
COPY --from=build /out/backend /backend
USER 1654:1654
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s CMD ["/backend", "healthcheck"]
ENTRYPOINT ["/backend"]
