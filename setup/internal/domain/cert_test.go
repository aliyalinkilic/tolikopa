package domain

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"
)

func makeCert(t *testing.T, tmpl *x509.Certificate, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if parent == nil {
		parent, parentKey = tmpl, key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	return c, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func leafTemplate() *x509.Certificate {
	return &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "tolikopa.corp.local"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"tolikopa.corp.local"},
		IPAddresses:           []net.IP{net.ParseIP("10.0.0.10")},
	}
}

func TestSelfSignedLeafIsTrustable(t *testing.T) {
	_, _, p := makeCert(t, leafTemplate(), nil, nil)
	pc, err := ParsePanelCert(p)
	if err != nil {
		t.Fatal(err)
	}
	if !pc.Trustable {
		t.Fatalf("self-signed server certificate refused: %s", pc.Reason)
	}
	if len(pc.Thumbprint) != 40 || len(pc.SHA256) != 64 {
		t.Errorf("thumbprints: %q %q", pc.Thumbprint, pc.SHA256)
	}
	if !pc.CoversName("TOLIKOPA.corp.local") || pc.CoversName("other.corp.local") {
		t.Error("CoversName")
	}
	if !pc.CoversIP("10.0.0.10") || pc.CoversIP("10.0.0.11") {
		t.Error("CoversIP")
	}
	if n, ip := pc.DefaultTarget(); n != "tolikopa.corp.local" || ip != "10.0.0.10" {
		t.Errorf("DefaultTarget = %q %q", n, ip)
	}
}

// A CA in the domain's trusted root store could vouch for any web site, so it
// is never distributed — whatever else the certificate looks like.
func TestCertificateAuthorityIsRefused(t *testing.T) {
	tmpl := leafTemplate()
	tmpl.IsCA = true
	tmpl.KeyUsage |= x509.KeyUsageCertSign
	_, _, p := makeCert(t, tmpl, nil, nil)
	pc, err := ParsePanelCert(p)
	if err != nil {
		t.Fatal(err)
	}
	if pc.Trustable {
		t.Fatal("a CA certificate was accepted for distribution")
	}
}

func TestIssuedCertificateIsRefused(t *testing.T) {
	caTmpl := leafTemplate()
	caTmpl.Subject.CommonName = "Some CA"
	caTmpl.IsCA = true
	caTmpl.KeyUsage = x509.KeyUsageCertSign
	ca, caKey, _ := makeCert(t, caTmpl, nil, nil)
	leaf := leafTemplate()
	leaf.SerialNumber = big.NewInt(2)
	_, _, p := makeCert(t, leaf, ca, caKey)
	pc, err := ParsePanelCert(p)
	if err != nil {
		t.Fatal(err)
	}
	if pc.Trustable {
		t.Fatal("a certificate issued by another CA was accepted for distribution")
	}
}

func TestExpiredOrClientCertificateIsRefused(t *testing.T) {
	expired := leafTemplate()
	expired.NotBefore = time.Now().Add(-48 * time.Hour)
	expired.NotAfter = time.Now().Add(-24 * time.Hour)
	client := leafTemplate()
	client.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	for name, tmpl := range map[string]*x509.Certificate{"expired": expired, "client": client} {
		_, _, p := makeCert(t, tmpl, nil, nil)
		pc, err := ParsePanelCert(p)
		if err != nil {
			t.Fatal(err)
		}
		if pc.Trustable {
			t.Errorf("%s certificate accepted", name)
		}
	}
	if _, err := ParsePanelCert([]byte("not pem")); err == nil {
		t.Error("garbage accepted")
	}
}
