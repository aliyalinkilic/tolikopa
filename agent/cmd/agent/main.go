// Command agent is the Tolikopa monitoring agent: a single static binary that runs
// as a Windows service (LocalSystem) or a Linux systemd unit, collecting the
// allowlisted system metrics in docs/AGENT.md and POSTing them to the server.
//
// Modes (by first argument):
//
//	install     register the service (delayed-auto-start, restart-on-failure)
//	uninstall   stop and remove the service
//	start|stop  control the installed service
//	run         run in the foreground (development / systemd ExecStart)
//	version     print the agent version (used by bootstrap.ps1 self-update)
//	--fg-helper    in-session helper (foreground app names + idle seconds)
//	--idle-helper  legacy stub; idle now rides --fg-helper's "__idle" field
//	--appx-worker  one-shot Appx removal worker (spawned by the agent itself)
//
// With no argument, under the Windows SCM the process runs as the service;
// otherwise it prints usage.
package main

import (
	"fmt"
	"os"

	"github.com/aliyalinkilic/tolikopa/agent/internal/actuator"
	"github.com/aliyalinkilic/tolikopa/agent/internal/config"
	"github.com/aliyalinkilic/tolikopa/agent/internal/service"
)

// version is overridable at build time: -ldflags "-X main.version=1.2.3".
var version = "2.0.0"

func main() {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "version", "--version", "-v":
		fmt.Println(version)
		return
	case "--idle-helper":
		// Legacy stub: idle detection is now carried by --fg-helper's "__idle"
		// control field. Exit cleanly so any old launcher never errors.
		fmt.Println("idle-helper: superseded by --fg-helper, nothing to do")
		return
	case "--appx-worker":
		// Short-lived child of the agent that performs ONE Appx package operation
		// and exits (see internal/actuator/appx_windows.go for why removal runs
		// out of process). It speaks one JSON line on stdin/stdout, needs no
		// config and holds no token, and re-checks the package allowlist itself.
		os.Exit(actuator.RunAppxWorker())
	case "--fg-helper":
		// In-session helper spawned by the agent (CreateProcessAsUser). Runs as the
		// interactive user and reports foreground app-usage by NAME only, plus
		// seconds-since-last-input ("__idle"). Needs no config (the agent, not the
		// helper, holds the ingest token).
		if err := service.RunForegroundHelper(); err != nil {
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load()
	if err != nil {
		fatalf("yapilandirma yuklenemedi: %v", err)
	}
	log := config.NewLogger(cfg.LogLevel)

	switch cmd {
	case "install":
		mustDo(service.Install(), "Tolikopa servisi kuruldu.")
	case "uninstall":
		mustDo(service.Uninstall(), "Tolikopa servisi kaldirildi.")
	case "start":
		mustDo(service.StartService(), "Tolikopa servisi baslatildi.")
	case "stop":
		mustDo(service.StopService(), "Tolikopa servisi durduruldu.")
	case "run":
		if err := service.RunForeground(cfg, log, version); err != nil {
			fatalf("calisma hatasi: %v", err)
		}
	default:
		// No (or unrecognized) subcommand: under the Windows SCM this is the
		// service entry point; elsewhere show usage.
		if isSvc, derr := service.IsService(); derr == nil && isSvc {
			if err := service.RunService(cfg, log, version); err != nil {
				fatalf("servis hatasi: %v", err)
			}
			return
		}
		if cmd != "" {
			fmt.Fprintf(os.Stderr, "bilinmeyen komut: %s\n\n", cmd)
		}
		usage()
		if cmd != "" {
			os.Exit(2)
		}
	}
}

func mustDo(err error, okMsg string) {
	if err != nil {
		fatalf("%v", err)
	}
	fmt.Println(okMsg)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func usage() {
	fmt.Printf(`Tolikopa Ajani %s

Kullanim:
  tolikopa-agent install      Servisi kurar
  tolikopa-agent uninstall    Servisi kaldirir
  tolikopa-agent start        Servisi baslatir
  tolikopa-agent stop         Servisi durdurur
  tolikopa-agent run          On planda calistirir (test/systemd)
  tolikopa-agent version      Surumu yazdirir
`, version)
}
