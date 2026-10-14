package domain

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/aliyalinkilic/tolikopa/setup/internal/payload"
)

// PanelCert is the server's TLS certificate as carried in the bundle's
// config.json (tls_ca_pem, the value the agents pin).
//
// The tool distributes it to the domain as a trusted root, so that browsers on
// domain PCs open the panel without a warning. A trusted root is powerful: a CA
// placed there could vouch for any web site. That is why only a self-signed,
// non-CA server certificate is ever accepted for this — it vouches for nothing
// but itself, so the worst a stolen server key allows is impersonating the
// panel's own names.
//
// Which names those are is checked too: a self-signed leaf for
// login.microsoftonline.com or *.corp.example.com is just as dangerous as a CA
// once every PC trusts it. A trustable certificate names exactly one DNS host
// and at most one IP address (what setup.sh generates), carries no wildcard,
// and is bound to the server the bundle points at: its DNS name is the host of
// server_url, or — when server_url is an address — its IP is that address.
type PanelCert struct {
	DER        []byte
	Thumbprint string // SHA-1, upper-case hex: the key Windows stores it under
	SHA256     string // what a browser shows as the fingerprint
	DNSNames   []string
	IPs        []string
	NotAfter   time.Time
	// Trustable is false when the certificate must not be distributed (a CA,
	// or one issued by someone else); Reason then says why.
	Trustable bool
	Reason    string
}

// LoadPanelCert reads the certificate from the bundle's config.json. It returns
// (nil, nil) when the config pins no certificate.
func LoadPanelCert(b *payload.Bundle) (*PanelCert, error) {
	var cfgFile *payload.File
	for i := range b.Manifest.Files {
		if b.Manifest.Files[i].Name == "config.json" {
			cfgFile = &b.Manifest.Files[i]
		}
	}
	if cfgFile == nil {
		return nil, nil
	}
	data, err := b.Read(*cfgFile)
	if err != nil {
		return nil, err
	}
	var cfg struct {
		TLSCAPem string `json:"tls_ca_pem"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config.json: %w", err)
	}
	if strings.TrimSpace(cfg.TLSCAPem) == "" {
		return nil, nil
	}
	return ParsePanelCert([]byte(cfg.TLSCAPem), b.Manifest.ServerURL)
}

// ParsePanelCert parses PEM text and decides whether it may be distributed.
// serverURL is the manifest's server_url: the certificate must belong to it.
func ParsePanelCert(pemText []byte, serverURL string) (*PanelCert, error) {
	var certs []*x509.Certificate
	rest := pemText
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("tls_ca_pem: %w", err)
		}
		certs = append(certs, c)
	}
	if len(certs) == 0 {
		return nil, errors.New("tls_ca_pem: no certificate")
	}
	c := certs[0]
	sha1Sum := sha1.Sum(c.Raw)
	sha256Sum := sha256.Sum256(c.Raw)
	pc := &PanelCert{
		DER:        c.Raw,
		Thumbprint: strings.ToUpper(hex.EncodeToString(sha1Sum[:])),
		SHA256:     strings.ToUpper(hex.EncodeToString(sha256Sum[:])),
		DNSNames:   c.DNSNames,
		NotAfter:   c.NotAfter,
	}
	for _, ip := range c.IPAddresses {
		pc.IPs = append(pc.IPs, ip.String())
	}
	switch {
	case len(certs) != 1:
		pc.Reason = "more than one certificate"
	case c.IsCA || c.KeyUsage&x509.KeyUsageCertSign != 0:
		pc.Reason = "it is a certificate authority"
	case !bytes.Equal(c.RawSubject, c.RawIssuer) || c.CheckSignature(c.SignatureAlgorithm, c.RawTBSCertificate, c.Signature) != nil:
		pc.Reason = "it is not self-signed"
	case !hasServerAuth(c):
		pc.Reason = "it is not a TLS server certificate"
	case hasWildcard(c.DNSNames):
		pc.Reason = "it carries a wildcard name"
	case len(c.DNSNames) == 0:
		pc.Reason = "it names no DNS host"
	case len(c.DNSNames) != 1:
		pc.Reason = "it names more than one DNS host"
	case !payload.ValidHostName(strings.ToLower(c.DNSNames[0])):
		pc.Reason = "its DNS name is not a valid host name"
	case len(c.IPAddresses) > 1:
		pc.Reason = "it names more than one IP address"
	case len(c.URIs) > 0 || len(c.EmailAddresses) > 0:
		pc.Reason = "it carries names other than a DNS host and an IP address"
	default:
		if r := serverMismatch(c, serverURL); r != "" {
			pc.Reason = r
		} else if time.Now().After(c.NotAfter) {
			pc.Reason = "it has expired"
		} else {
			pc.Trustable = true
		}
	}
	return pc, nil
}

func hasWildcard(names []string) bool {
	for _, n := range names {
		if strings.Contains(n, "*") {
			return true
		}
	}
	return false
}

// serverMismatch ties the certificate to the server the bundle names. The DNS
// name may be any name the admin chose for the panel (inside the AD domain or
// not), but it must be the one the agents and browsers are sent to. It returns
// "" when the certificate matches.
func serverMismatch(c *x509.Certificate, serverURL string) string {
	u, err := url.Parse(serverURL)
	if serverURL == "" || err != nil || u.Hostname() == "" {
		return "the bundle names no server to match it against"
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if len(c.IPAddresses) != 1 || !c.IPAddresses[0].Equal(ip) {
			return "its IP address is not the server's address " + host
		}
		return ""
	}
	if !strings.EqualFold(c.DNSNames[0], host) {
		return "its DNS name is not the server's name " + strings.ToLower(host)
	}
	return ""
}

func hasServerAuth(c *x509.Certificate) bool {
	for _, u := range c.ExtKeyUsage {
		if u == x509.ExtKeyUsageServerAuth {
			return true
		}
	}
	return false
}

// CoversName reports whether the certificate is valid for host.
func (p *PanelCert) CoversName(host string) bool {
	for _, n := range p.DNSNames {
		if strings.EqualFold(n, host) {
			return true
		}
	}
	return false
}

// CoversIP reports whether the certificate is valid for ip.
func (p *PanelCert) CoversIP(ip string) bool {
	want := net.ParseIP(ip)
	for _, s := range p.IPs {
		if want != nil && net.ParseIP(s).Equal(want) {
			return true
		}
	}
	return false
}

// DefaultTarget is the DNS name and address the record should get unless the
// admin types others: the certificate's first name and first IPv4 address.
func (p *PanelCert) DefaultTarget() (name, ip string) {
	if len(p.DNSNames) > 0 {
		name = strings.ToLower(p.DNSNames[0])
	}
	for _, s := range p.IPs {
		if v := net.ParseIP(s); v != nil && v.To4() != nil {
			return name, v.To4().String()
		}
	}
	return name, ""
}
