# syntax=docker/dockerfile:1
#
# Tolikopa API image. Build context is the REPO ROOT (compose: context: ..,
# dockerfile: server/Dockerfile), because besides the server it compiles what the
# panel hands out during setup: the Windows and Linux agents and the domain rollout
# tool. Rebuilding the image after a `git pull` is therefore also what produces a
# new agent build; the panel then offers it in tolikopa-update.zip.
#
# Supply chain: this pipeline builds the binary that runs as SYSTEM on every PC.
#  - Base images are pinned by tag AND digest (tag for humans, digest is what
#    Docker actually pulls). To update: `docker buildx imagetools inspect <tag>`
#    and replace both.
#  - Go dependencies come only from the committed go.mod/go.sum: `go mod download`
#    (verified against go.sum and the checksum database) + `-mod=readonly`. No
#    `go mod tidy` here: it would resolve a new import to whatever is latest.
#  - GOTOOLCHAIN=local: a go.mod asking for a newer Go fails the build instead of
#    silently downloading a different toolchain.
ARG GO_IMAGE=golang:1.26.9-trixie@sha256:f06b7122f888680048afb247c4e5f49c0069eb6c4c1ccae0f3f4834527a8d22c
ARG RUNTIME_IMAGE=alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

# ── Agent + rollout tool ───────────────────────────────────────────────────────
FROM ${GO_IMAGE} AS artifacts
ENV CGO_ENABLED=0 GOFLAGS=-mod=readonly GOTOOLCHAIN=local
WORKDIR /src

COPY agent/go.mod agent/go.sum ./agent/
RUN cd agent && go mod download && go mod verify
COPY agent/ ./agent/
# Windows version resource + application manifest (asInvoker) for both
# executables: a .syso next to main.go that `go build` links in for
# GOOS=windows only. go-winres is a build-time tool, pinned to an exact version
# and fetched through the module proxy, verified against the checksum database;
# it is never a dependency of the shipped modules. `go run pkg@version` ignores
# the surrounding go.mod, so GOFLAGS=-mod=readonly does not get in its way. The
# input is the committed winres/winres.json; only the version comes from here.
# Same input, same .syso: the generation is deterministic.
ARG GO_WINRES=github.com/tc-hib/go-winres@v0.3.3
RUN AGENT_VERSION="$(sed -n 's/^var version = "\(.*\)"$/\1/p' agent/cmd/agent/main.go)" \
 && test -n "$AGENT_VERSION" \
 && cd agent/cmd/agent \
 && go run "$GO_WINRES" make --in winres/winres.json --arch amd64 \
      --product-version "$AGENT_VERSION" --file-version "$AGENT_VERSION" \
 && test -f rsrc_windows_amd64.syso
RUN cd agent \
 && GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o /out/tolikopa-agent.exe ./cmd/agent \
 && GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o /out/tolikopa-agent     ./cmd/agent

# The rollout tool is stdlib-only (go.mod without requires, hence no go.sum).
COPY VERSION ./VERSION
COPY setup/ ./setup/
RUN cd setup/cmd/tolikopa-setup \
 && go run "$GO_WINRES" make --in winres/winres.json --arch amd64 \
      --product-version "$(cat /src/VERSION)" --file-version "$(cat /src/VERSION)" \
 && test -f rsrc_windows_amd64.syso
RUN cd setup && GOOS=windows GOARCH=amd64 go build -trimpath \
      -ldflags "-s -w -X main.version=$(cat ../VERSION)" -o /out/tolikopa-setup.exe ./cmd/tolikopa-setup

COPY deploy/gpo/bootstrap.ps1 /out/bootstrap.ps1
# The agent's version is the default compiled into cmd/agent/main.go.
RUN AGENT_VERSION="$(sed -n 's/^var version = "\(.*\)"$/\1/p' agent/cmd/agent/main.go)" \
 && test -n "$AGENT_VERSION" \
 && printf '{"product_version":"%s","agent_version":"%s","built_at":"%s"}\n' \
      "$(cat VERSION)" "$AGENT_VERSION" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > /out/artifacts.json

# ── Server ─────────────────────────────────────────────────────────────────────
FROM ${GO_IMAGE} AS build
ENV CGO_ENABLED=0 GOFLAGS=-mod=readonly GOTOOLCHAIN=local
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download && go mod verify
# Migrations are embedded in the binary via embed.FS (no runtime copy needed).
COPY VERSION /VERSION
COPY server/ .
RUN go build -trimpath \
      -ldflags "-s -w -X github.com/aliyalinkilic/tolikopa/server/internal/buildinfo.Version=$(cat /VERSION)" \
      -o /out/server ./cmd/server

# ── Runtime ────────────────────────────────────────────────────────────────────
FROM ${RUNTIME_IMAGE}
RUN apk add --no-cache ca-certificates tzdata wget
COPY --from=build /out/server /server
COPY --from=artifacts /out/ /opt/tolikopa/artifacts/
ENV TZ=Europe/Istanbul \
    ARTIFACTS_DIR=/opt/tolikopa/artifacts
EXPOSE 8080
# Unprivileged: the server only reads its binary, the artifacts and the CA certificate.
USER 65534:65534
ENTRYPOINT ["/server"]
