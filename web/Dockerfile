# Tolikopa web panel — build the SPA, serve it with Caddy.
# Build context is the REPO ROOT (compose: context: .., dockerfile: web/Dockerfile).

# ── Build stage ────────────────────────────────────────────────────────────────
FROM node:22-alpine AS build
WORKDIR /app
COPY web/package*.json ./
RUN npm install
COPY web/ ./
RUN npm run build

# ── Serve stage ────────────────────────────────────────────────────────────────
FROM caddy:2.11-alpine
COPY --from=build /app/dist /srv
COPY deploy/caddy/Caddyfile /etc/caddy/Caddyfile
EXPOSE 80 443
