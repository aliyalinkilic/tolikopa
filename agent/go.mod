module github.com/aliyalinkilic/tolikopa/agent

go 1.26

// The require blocks and go.sum are committed and complete: the image build runs
// `go mod download` + `go build -mod=readonly`, never `go mod tidy`, so the binary
// that runs as SYSTEM on every PC is built from exactly these versions. After
// adding or removing an import, run `go mod tidy` locally and commit both files.
// Primary deps:
//   github.com/shirou/gopsutil/v4   — CPU/RAM/disk/net/process metrics
//   golang.org/x/sys/windows        — svc, mgr, WTS session APIs, wevtapi
//   github.com/yusufpapurcu/wmi     — WMI (SMART health)
//   github.com/google/uuid          — stable agent_id

require (
	github.com/go-ole/go-ole v1.2.6
	github.com/google/uuid v1.6.0
	github.com/shirou/gopsutil/v4 v4.26.6
	github.com/yusufpapurcu/wmi v1.2.4
	golang.org/x/sys v0.46.0
)

require (
	github.com/ebitengine/purego v0.10.0 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/tklauser/go-sysconf v0.3.16 // indirect
	github.com/tklauser/numcpus v0.11.0 // indirect
)
