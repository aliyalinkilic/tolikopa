# syntax=docker/dockerfile:1
#
# Tolikopa API image. Build context is the REPO ROOT (compose: context: ..,
# dockerfile: server/Dockerfile), because besides the server it compiles what the
# panel hands out during setup: the Windows and Linux agents and the domain rollout
# tool. Rebuilding the image after a `git pull` is therefore also what produces a
# new agent build; the panel then offers it as tolikopa-update.exe.

# ── Agent + rollout tool ───────────────────────────────────────────────────────
FROM golang:1.26 AS artifacts
ENV CGO_ENABLED=0
WORKDIR /src

# The committed go.mod files are intentionally minimal (no require block);
# `go mod tidy` populates requires + go.sum from the source.
COPY agent/ ./agent/
RUN cd agent && go mod tidy \
 && GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o /out/tolikopa-agent.exe ./cmd/agent \
 && GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o /out/tolikopa-agent     ./cmd/agent

COPY VERSION ./VERSION
COPY setup/ ./setup/
RUN cd setup && GOOS=windows GOARCH=amd64 go build -trimpath \
      -ldflags "-s -w -X main.version=$(cat ../VERSION)" -o /out/tolikopa-setup.exe ./cmd/tolikopa-setup

COPY deploy/gpo/bootstrap.ps1 /out/bootstrap.ps1
# The agent's version is the default compiled into cmd/agent/main.go.
RUN AGENT_VERSION="$(sed -n 's/^var version = "\(.*\)"$/\1/p' agent/cmd/agent/main.go)" \
 && test -n "$AGENT_VERSION" \
 && printf '{"product_version":"%s","agent_version":"%s","built_at":"%s"}\n' \
      "$(cat VERSION)" "$AGENT_VERSION" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > /out/artifacts.json

# ── Server ─────────────────────────────────────────────────────────────────────
FROM golang:1.26 AS build
WORKDIR /src
# Migrations are embedded in the binary via embed.FS (no runtime copy needed).
COPY VERSION /VERSION
COPY server/ .
RUN go mod tidy
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X github.com/aliyalinkilic/tolikopa/server/internal/buildinfo.Version=$(cat /VERSION)" \
      -o /out/server ./cmd/server

# ── Runtime ────────────────────────────────────────────────────────────────────
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata wget
COPY --from=build /out/server /server
COPY --from=artifacts /out/ /opt/tolikopa/artifacts/
ENV TZ=Europe/Istanbul \
    ARTIFACTS_DIR=/opt/tolikopa/artifacts
EXPOSE 8080
# Unprivileged: the server only reads its binary, the artifacts and the CA certificate.
USER 65534:65534
ENTRYPOINT ["/server"]
