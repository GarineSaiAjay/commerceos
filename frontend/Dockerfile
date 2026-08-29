# frontend/Dockerfile
#
# Standard multi-stage Next.js build. There was previously no way to run
# the frontend in Docker at all (infra/docker-compose.yml only ever
# defined postgres/redis/backend) -- README's own "Getting started"
# listed "frontend :3000" as if `docker compose up` started it, when it
# only ever came up via a separate manual `npm install && npm run dev`.
# This Dockerfile plus the frontend service in
# infra/docker-compose.yml close that gap.
#
# next.config.ts has no `output: "standalone"`, so the runtime stage
# copies node_modules wholesale rather than a pruned standalone bundle
# -- simpler and safe for a project this size; revisit only if image
# size becomes an actual constraint.

FROM node:22-slim AS deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci

FROM node:22-slim AS build
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN npm run build

FROM node:22-slim AS run
WORKDIR /app
ENV NODE_ENV=production
COPY --from=build /app/public ./public
COPY --from=build /app/.next ./.next
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/package.json ./package.json
COPY --from=build /app/next.config.ts ./next.config.ts

EXPOSE 3000
CMD ["npm", "run", "start"]
