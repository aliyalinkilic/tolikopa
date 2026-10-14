// Package buildinfo carries the server's version string.
package buildinfo

// Version is reported by GET /api/settings/public. Override at build time:
//
//	go build -ldflags "-X github.com/aliyalinkilic/tolikopa/server/internal/buildinfo.Version=1.2.3" ./cmd/server
var Version = "dev"
