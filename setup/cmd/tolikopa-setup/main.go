// Command tolikopa-setup deploys the Tolikopa agent to an Active Directory domain.
//
// An IT administrator downloads it from the panel and runs it on their own
// domain-joined PC (or on a domain controller). The download carries a bundle —
// tolikopa-agent.exe, bootstrap.ps1, config.json and a manifest — appended to this
// executable. The same binary served as tolikopa-update.exe carries an "update"
// bundle, which only refreshes the files on the share.
//
// A setup run also gives the panel its DNS name in the domain's DNS and makes
// domain PCs trust the panel's self-signed certificate through the same GPO.
//
// It acts with the rights of the person running it and nothing else: no password
// is asked for, stored, or sent anywhere, and it makes no network connection to
// the Tolikopa server.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"runtime"
	"strings"

	"github.com/aliyalinkilic/tolikopa/setup/internal/domain"
	"github.com/aliyalinkilic/tolikopa/setup/internal/payload"
)

var version = "dev"

var (
	lang  = "en"
	stdin = bufio.NewReader(os.Stdin)
)

func t(key string, args ...any) string {
	s, ok := messages[lang][key]
	if !ok {
		s = messages["en"][key]
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

type options struct {
	dryRun, yes, remove, skipDNS bool
	bundlePath, lang             string
	dnsName, dnsIP               string
}

func main() {
	var o options
	flag.BoolVar(&o.dryRun, "dry-run", false, "show what would be done, change nothing")
	flag.BoolVar(&o.yes, "yes", false, "do not ask anything; use the defaults")
	flag.BoolVar(&o.remove, "remove", false, "remove the GPO and the NETLOGON folder")
	flag.StringVar(&o.bundlePath, "bundle", "", "read the deployment bundle from this zip instead of the executable itself")
	flag.StringVar(&o.lang, "lang", "", "console language: tr or en")
	flag.StringVar(&o.dnsName, "dns-name", "", "the panel's DNS name (default: the name in its certificate)")
	flag.StringVar(&o.dnsIP, "dns-ip", "", "the address the DNS name points to (default: the address in the certificate)")
	flag.BoolVar(&o.skipDNS, "skip-dns", false, "do not create or check the panel's DNS record")
	showVersion := flag.Bool("version", false, "print the tool version")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	enableUTF8Console()

	// Double-clicked from Explorer the console closes with the process; hold it
	// open so the outcome can be read.
	code := run(o)
	if !o.yes {
		fmt.Println()
		fmt.Print(t("press_enter"))
		_, _ = stdin.ReadString('\n')
	}
	os.Exit(code)
}

func run(o options) int {
	if o.lang == "tr" || o.lang == "en" {
		lang = o.lang
	}
	if o.bundlePath == "" {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		o.bundlePath = exe
	}
	b, err := payload.Open(o.bundlePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, t("bundle_error"))
		fmt.Fprintf(os.Stderr, "%s: %v\n", t("err_detail"), err)
		return 1
	}
	defer b.Close()
	m := &b.Manifest
	if o.lang == "" && (m.Lang == "tr" || m.Lang == "en") {
		lang = m.Lang
	}
	setupRun := m.Mode == payload.ModeSetup && !o.remove

	switch {
	case o.remove:
		fmt.Println(t("banner_remove"))
	case m.Mode == payload.ModeUpdate:
		fmt.Println(t("banner_update"))
	default:
		fmt.Println(t("banner_setup"))
	}
	fmt.Println(strings.Repeat("=", 64))
	if o.dryRun {
		fmt.Println(t("dry_run"))
	}
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, t("no_windows"))
		return 1
	}

	// The panel certificate travels in config.json (the agents pin it). A setup
	// run distributes it as a trusted root when it is safe to (see PanelCert).
	var cert *domain.PanelCert
	if setupRun {
		if cert, err = domain.LoadPanelCert(b); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", t("err_detail"), err)
			return 1
		}
	}

	var dns *domain.DNSTarget
	if setupRun && !o.skipDNS {
		if dns, err = chooseDNS(o, m, cert); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}

	fmt.Println(t("checking"))
	pre, err := domain.Preflight(m, dns)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", t("err_detail"), err)
		return 1
	}
	row := func(label, value string) { fmt.Printf("  %-16s %s\n", label+":", value) }
	row(t("computer"), pre.Computer)
	row(t("signed_in_as"), pre.User)
	if m.ServerURL != "" {
		row(t("server"), m.ServerURL)
	}
	if m.AgentVersion != "" {
		row(t("agent_version"), m.AgentVersion)
	}
	// The panel shows the same hash; comparing the two is how an admin confirms the
	// agent in this download is the build the server reports.
	for _, f := range m.Files {
		if f.Name == "tolikopa-agent.exe" {
			row("SHA-256", f.SHA256)
		}
	}
	if cert != nil {
		row(t("certificate"), strings.Join(append(append([]string{}, cert.DNSNames...), cert.IPs...), ", "))
		row(t("cert_fingerprint"), cert.SHA256)
		row(t("cert_expires"), cert.NotAfter.Format("2006-01-02"))
		if !cert.Trustable {
			fmt.Println("  " + t("cert_not_trusted", cert.Reason))
		}
	}
	fmt.Println()

	needGPO := o.remove || m.Mode == payload.ModeSetup
	blocked := false
	block := func(msg string) {
		if !blocked {
			fmt.Fprintln(os.Stderr, msg)
		}
		blocked = true
	}
	switch {
	case !pre.PartOfDomain:
		block(t("err_not_domain"))
	case !pre.DomainMatch:
		block(t("err_wrong_domain", pre.ComputerDomain, m.DomainFQDN))
	case !pre.ShareParentReachable:
		block(t("err_share", `\\`+m.DomainFQDN+`\NETLOGON`))
	case needGPO && !pre.GPModule:
		block(t("err_gp_module"))
	case needGPO && m.GPOName == "":
		block(t("err_remove_bundle"))
	case needGPO && pre.GPOForeign:
		block(t("err_gpo_in_use", m.GPOName))
	case dns != nil && !pre.DNSModule:
		block(t("err_dns_module"))
	case dns != nil && pre.DNSState == "conflict":
		block(t("err_dns_conflict", dns.Name, strings.Join(limit(pre.DNSExisting, 5), ", ")))
	case dns != nil && pre.DNSState == "unknown":
		block(t("err_dns_unknown", pre.DNSError))
	}

	// The plan is printed even when a check failed, so a dry run on any machine
	// still shows exactly what the tool intends to do.
	fmt.Println(t("plan") + ":")
	step := 0
	item := func(s string) {
		step++
		fmt.Printf("  %d. %s\n", step, s)
	}
	var names []string
	for _, f := range m.Files {
		names = append(names, f.Name)
	}
	switch {
	case o.remove:
		item(t("plan_rm_gpo", m.GPOName))
		item(t("plan_rm_share", m.SharePath()))
		fmt.Println("  !  " + t("plan_rm_note"))
	default:
		item(t("plan_copy", strings.Join(names, ", "), m.SharePath()))
		if dns != nil {
			switch pre.DNSState {
			case "present":
				item(t("plan_dns_present", dns.Name, dns.IP))
			case "new_zone":
				item(t("plan_dns_zone", dns.Name, dns.IP))
			default:
				item(t("plan_dns_add", dns.Name, dns.IP, pre.DNSZone))
			}
		}
		if m.Mode == payload.ModeSetup {
			if pre.GPOExists {
				item(t("plan_gpo_reuse", m.GPOName))
			} else {
				item(t("plan_gpo", m.GPOName))
			}
			if cert != nil && cert.Trustable {
				item(t("plan_trust", strings.Join(cert.DNSNames, ", ")))
			}
			if m.DefenderExclusion {
				item(t("plan_defender", `%ProgramData%\Tolikopa\tolikopa-agent.exe`))
			}
			item(t("plan_link", m.LinkTarget()))
		}
	}
	fmt.Println()

	if blocked {
		return 1
	}
	if o.dryRun {
		return 0
	}
	// Endpoint-hygiene opt-ins: asked here, on the admin's machine, and never read
	// from the bundle. An update keeps what the share already says.
	sw := domain.CurrentSwitches(m)
	if setupRun && !o.yes {
		fmt.Println(t("switch_intro"))
		sw.AllowAppRemoval = ask(t("switch_apps"), sw.AllowAppRemoval)
		sw.AllowStartupControl = ask(t("switch_startup"), sw.AllowStartupControl)
		if sw.AllowAppRemoval || sw.AllowStartupControl {
			sw.EnforceActions = ask(t("switch_enforce"), sw.EnforceActions)
		} else {
			sw.EnforceActions = false
		}
		fmt.Println()
	}

	if !o.yes {
		fmt.Print(t("confirm"))
		answer, _ := stdin.ReadString('\n')
		if !strings.EqualFold(strings.TrimSpace(answer), t("confirm_word")) {
			fmt.Println(t("cancelled"))
			return 0
		}
	}

	if o.remove {
		res, err := domain.RemoveGPO(m)
		if code := report(m, res, err); code != 0 {
			return code
		}
		if err := domain.RemoveShare(m); err != nil {
			return report(m, nil, err)
		}
		fmt.Println(t("done_remove"))
		return 0
	}

	fmt.Println(t("step_copy"))
	changed, err := domain.Publish(b, sw)
	if err != nil {
		return report(m, nil, err)
	}
	isChanged := map[string]bool{}
	for _, n := range changed {
		isChanged[n] = true
	}
	for _, n := range domain.PublishOrder {
		for _, f := range m.Files {
			if f.Name != n {
				continue
			}
			state := t("up_to_date")
			if isChanged[n] {
				state = t("copied")
			}
			fmt.Printf("  %-18s %s\n", n, state)
		}
	}

	if m.Mode == payload.ModeUpdate {
		fmt.Println()
		fmt.Println(t("done_update"))
		return 0
	}

	// DNS before the GPO: the agents the GPO installs look the panel up by name.
	if dns != nil {
		fmt.Println(t("step_dns"))
		res, err := domain.ApplyDNS(m, *dns)
		if code := report(m, res, err); code != 0 {
			return code
		}
		switch res.DNSState {
		case "present":
			fmt.Printf("  %s\n", t("dns_present", dns.Name, dns.IP))
		case "new_zone":
			fmt.Printf("  %s\n", t("dns_zone_created", dns.Name, dns.IP))
		default:
			fmt.Printf("  %s\n", t("dns_added", dns.Name, dns.IP, res.DNSZone))
		}
	}

	fmt.Println(t("step_gpo"))
	res, err := domain.ApplyGPO(m, cert)
	if code := report(m, res, err); code != 0 {
		return code
	}
	switch {
	case res.GPOCreated:
		fmt.Printf("  %s (%s)\n", t("gpo_created"), res.GPOID)
	case res.TaskUpdated:
		fmt.Printf("  %s (%s)\n", t("gpo_updated"), res.GPOID)
	default:
		fmt.Printf("  %s (%s)\n", t("gpo_unchanged"), res.GPOID)
	}
	if res.TrustInstalled {
		fmt.Printf("  %s\n", t("trust_done"))
	}
	if res.LinkCreated {
		fmt.Printf("  %s %s\n", t("link_created"), res.Target)
	} else {
		fmt.Printf("  %s %s\n", t("link_present"), res.Target)
	}
	fmt.Println()
	fmt.Println(t("done_setup"))
	return 0
}

// chooseDNS settles the record to create: flags first, then the certificate,
// then the server address; in an interactive run the admin may type others.
// nil means the admin chose to skip the DNS step.
func chooseDNS(o options, m *payload.Manifest, cert *domain.PanelCert) (*domain.DNSTarget, error) {
	name, ip := "", ""
	if cert != nil {
		name, ip = cert.DefaultTarget()
	}
	if u, err := url.Parse(m.ServerURL); err == nil && u.Hostname() != "" {
		h := u.Hostname()
		if net.ParseIP(h) != nil {
			if ip == "" {
				ip = h
			}
		} else if name == "" {
			name = strings.ToLower(h)
		}
	}
	if o.dnsName != "" {
		name = strings.ToLower(strings.TrimSpace(o.dnsName))
	}
	if o.dnsIP != "" {
		ip = strings.TrimSpace(o.dnsIP)
	}

	nameErr := func(n string) string {
		if !payload.ValidHostName(n) {
			return t("dns_name_invalid")
		}
		if cert != nil && cert.Trustable && !cert.CoversName(n) {
			return t("dns_name_not_in_cert", strings.Join(cert.DNSNames, ", "))
		}
		return ""
	}
	ipErr := func(a string) string {
		if !payload.ValidIPv4(a) {
			return t("dns_ip_invalid")
		}
		return ""
	}

	if o.yes || o.dryRun {
		if name == "" || ip == "" {
			fmt.Println(t("dns_skipped_no_default"))
			return nil, nil
		}
		if e := nameErr(name); e != "" {
			return nil, fmt.Errorf("%s: %s", name, e)
		}
		if e := ipErr(ip); e != "" {
			return nil, fmt.Errorf("%s: %s", ip, e)
		}
		return &domain.DNSTarget{Name: name, IP: ip}, nil
	}

	fmt.Println(t("dns_intro"))
	for {
		v := prompt(t("dns_name_prompt"), name)
		if v == "-" {
			fmt.Println(t("dns_skipped"))
			fmt.Println()
			return nil, nil
		}
		v = strings.ToLower(v)
		if e := nameErr(v); e != "" {
			fmt.Println("    " + e)
			continue
		}
		name = v
		break
	}
	for {
		v := prompt(t("dns_ip_prompt"), ip)
		if e := ipErr(v); e != "" {
			fmt.Println("    " + e)
			continue
		}
		ip = v
		break
	}
	if cert != nil && len(cert.IPs) > 0 && !cert.CoversIP(ip) {
		fmt.Println("    " + t("dns_ip_not_in_cert", strings.Join(cert.IPs, ", ")))
	}
	fmt.Println()
	return &domain.DNSTarget{Name: name, IP: ip}, nil
}

// prompt reads one line; Enter keeps def.
func prompt(question, def string) string {
	if def != "" {
		fmt.Printf("  %s [%s]: ", question, def)
	} else {
		fmt.Printf("  %s: ", question)
	}
	answer, _ := stdin.ReadString('\n')
	// A byte-order mark arrives when the answers are piped in from PowerShell.
	answer = strings.TrimSpace(strings.TrimPrefix(answer, string(rune(0xFEFF))))
	if answer == "" {
		return def
	}
	return answer
}

// ask reads a yes/no answer; Enter keeps the current value.
func ask(question string, current bool) bool {
	def := t("no")
	if current {
		def = t("yes")
	}
	fmt.Printf("  %s [%s/%s, Enter = %s]: ", question, t("yes"), t("no"), def)
	answer, _ := stdin.ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	switch {
	case answer == "":
		return current
	case strings.HasPrefix(answer, "y") || strings.HasPrefix(answer, "e"):
		return true
	default:
		return false
	}
}

func limit(s []string, n int) []string {
	if len(s) > n {
		return append(s[:n:n], "…")
	}
	return s
}

// report turns a failed step into a readable message and a non-zero exit code.
func report(m *payload.Manifest, res *domain.Result, err error) int {
	if err == nil && res != nil && res.OK {
		return 0
	}
	detail := ""
	if err != nil {
		detail = err.Error()
	} else if res != nil {
		detail = res.Error
	}
	switch detail {
	case "not_domain_joined":
		fmt.Fprintln(os.Stderr, t("err_not_domain"))
	case "gp_module_missing":
		fmt.Fprintln(os.Stderr, t("err_gp_module"))
	case "dns_module_missing":
		fmt.Fprintln(os.Stderr, t("err_dns_module"))
	case "dns_conflict":
		existing := ""
		if res != nil {
			existing = strings.Join(limit(res.DNSExisting, 5), ", ")
		}
		fmt.Fprintln(os.Stderr, t("err_dns_conflict", "?", existing))
	case "wrong_domain":
		fmt.Fprintln(os.Stderr, t("err_wrong_domain", "?", m.DomainFQDN))
	case "gpo_name_in_use":
		fmt.Fprintln(os.Stderr, t("err_gpo_in_use", m.GPOName))
	default:
		fmt.Fprintln(os.Stderr, t("err_rights"))
		fmt.Fprintf(os.Stderr, "%s: %s\n", t("err_detail"), detail)
	}
	return 1
}
