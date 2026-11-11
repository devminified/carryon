# CO-506: the self-hosted stack's web app image. Built from the repo
# root (docker-compose.yml's `web` service sets `context: ../..`) so
# this Dockerfile's COPY paths are relative to the real repo layout,
# not just web/ — `web/` is its own self-contained pnpm project (no
# root-level pnpm-workspace.yaml), so every pnpm command below still
# runs with web/ as its own working directory.
FROM node:22-alpine AS base
RUN corepack enable
WORKDIR /app/web

FROM base AS deps
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

FROM base AS builder
COPY --from=deps /app/web/node_modules ./node_modules
COPY web/ ./
# NEXT_PUBLIC_* values are baked in at build time by Next.js — real
# for this self-hosted image means building once per real deployment
# target, same as any other Next.js app; docker-compose.yml's own
# `build.context`/`args` would need to pass these through if a
# self-host operator's SUPABASE_URL isn't known yet at image-build
# time (see README.md's own note on rebuilding after first configuring .env).
ARG NEXT_PUBLIC_SUPABASE_URL
ARG NEXT_PUBLIC_SUPABASE_ANON_KEY
ARG NEXT_PUBLIC_AI_ENABLED
ENV NEXT_PUBLIC_SUPABASE_URL=${NEXT_PUBLIC_SUPABASE_URL}
ENV NEXT_PUBLIC_SUPABASE_ANON_KEY=${NEXT_PUBLIC_SUPABASE_ANON_KEY}
ENV NEXT_PUBLIC_AI_ENABLED=${NEXT_PUBLIC_AI_ENABLED}
RUN pnpm build

FROM base AS runner
ENV NODE_ENV=production
# CO-507: this stage never runs npm/npx/corepack (CMD below calls
# `next` directly — see its own comment), but `base`'s node:22-alpine
# image ships them anyway, each carrying its own vendored
# node_modules with real, if unreachable, CVEs (confirmed by a real
# trivy scan: ~10 HIGH findings in npm's own bundled brace-expansion/
# pacote/sigstore/etc., none in anything this image's own runtime
# path touches). Removed here, not in `deps`/`builder`, which still
# need them to actually build the app.
RUN rm -rf /usr/local/lib/node_modules/npm /usr/local/bin/npm /usr/local/bin/npx /usr/local/bin/corepack
COPY --from=builder /app/web/public ./public
COPY --from=builder /app/web/.next ./.next
COPY --from=builder /app/web/node_modules ./node_modules
COPY --from=builder /app/web/package.json ./package.json
EXPOSE 3000
# `next start` directly, not `pnpm start` — a real run showed `pnpm`
# itself isn't actually present in this runner stage (only
# `corepack enable` ran, in the `base` stage; the real pnpm binary is
# fetched lazily on first use, over the network) — this container
# booted fine with network access, but a self-hosted operator without
# it would see this stage fail for a reason that has nothing to do
# with the app itself.
CMD ["node_modules/.bin/next", "start"]
