# syntax=docker/dockerfile:1.7

# --- builder ---------------------------------------------------------------
# Builds the static web client out of the pnpm monorepo. The build context is
# the repo root (see compose.yml `build.context: ../..`) because the web app's
# workspace deps (@coding-anywhere/protocol, /markdown, ...) are consumed as
# TypeScript source and must be present to bundle.
FROM node:22-alpine AS builder

# Use the pnpm version pinned in package.json#packageManager.
RUN corepack enable

WORKDIR /src

# Copy the whole workspace, then install. node_modules / dist / .tsbuildinfo /
# .git are kept out of the context by the repo-root .dockerignore, so the
# context stays small and the composite workspace deps rebuild cleanly.
COPY . .

# Resilient install: extra retries + lower network concurrency ride out flaky
# registries, and the BuildKit cache mounts persist pnpm's store and corepack's
# download across builds so a dropped connection resumes from cache.
ENV npm_config_fetch_retries=5 \
    npm_config_fetch_retry_mintimeout=20000 \
    npm_config_fetch_retry_maxtimeout=120000 \
    npm_config_network_concurrency=4
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    --mount=type=cache,target=/root/.cache/node/corepack \
    for i in 1 2 3 4 5; do \
      pnpm install --frozen-lockfile && exit 0; \
      echo "pnpm install attempt $i failed; retrying in 15s..."; \
      sleep 15; \
    done; \
    exit 1

# Build the web app AND its workspace deps in topological order. The deps are
# TypeScript "composite" projects referenced by web's tsconfig, so their
# dist/*.d.ts must exist before web's `tsc` runs — the `...` suffix builds them
# first.
RUN pnpm --filter "@coding-anywhere/web..." run build

# --- runtime ---------------------------------------------------------------
FROM nginx:1.27-alpine

# SPA-aware config: root path, /guest fallback, immutable asset caching, and a
# never-cached service worker so clients pick up new builds.
COPY docker/web/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /src/apps/web/dist /usr/share/nginx/html

EXPOSE 80
