# syntax=docker/dockerfile:1
#
# Tolikopa web panel — build the SPA, serve it with Caddy.
# Build context is the REPO ROOT (compose: context: .., dockerfile: web/Dockerfile).
#
# Base images are pinned by tag AND digest (the digest is what Docker pulls). To
# update: `docker buildx imagetools inspect <tag>` and replace both.
ARG NODE_IMAGE=node:22.23.3-alpine3.24@sha256:0a7108bf6c7bf5de370ffb1a3ed6be93d405b43ff159f681a8d18c0e2bc2e402
ARG CADDY_IMAGE=caddy:2.11.4-alpine@sha256:6aeddd44c3078b0f9a35206472a11420648a79c184603ef95957d0a20044cb2b

# ── Build stage ────────────────────────────────────────────────────────────────
FROM ${NODE_IMAGE} AS build
WORKDIR /app
# npm ci installs exactly package-lock.json (integrity-checked) and fails if it
# does not match package.json; npm install could rewrite it.
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# The Caddyfile's Content-Security-Policy allows inline scripts by hash only. Fail
# the build when dist/index.html carries an inline script whose hash is not in
# the Caddyfile (someone edited the theme/language bootstrap in web/index.html):
# otherwise the panel would ship with that script silently blocked.
COPY deploy/caddy/Caddyfile /tmp/Caddyfile
RUN node <<'EOF'
const fs = require('fs')
const crypto = require('crypto')
const html = fs.readFileSync('dist/index.html', 'utf8')
const caddy = fs.readFileSync('/tmp/Caddyfile', 'utf8')
let bad = 0
for (const m of html.matchAll(/<script(?![^>]*\bsrc=)[^>]*>([\s\S]*?)<\/script>/g)) {
  const h = "'sha256-" + crypto.createHash('sha256').update(m[1], 'utf8').digest('base64') + "'"
  if (!caddy.includes(h)) {
    console.error('inline script in dist/index.html is not allowed by the CSP in deploy/caddy/Caddyfile; add ' + h + ' to script-src')
    bad++
  }
}
process.exit(bad ? 1 : 0)
EOF

# ── Serve stage ────────────────────────────────────────────────────────────────
FROM ${CADDY_IMAGE}
COPY --from=build /app/dist /srv
COPY deploy/caddy/Caddyfile /etc/caddy/Caddyfile
EXPOSE 80 443
