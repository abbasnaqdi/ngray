package system

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"
)

// GenSelfSigned writes a self-signed ECDSA cert/key for cn unless cert already
// exists. Honors the sandbox root. Used by the hysteria2 core and the
// subscription HTTPS endpoint. Pass extra IPs/hostnames in extraSANs to add
// them as Subject Alternative Names (needed when clients connect by IP).
func (e *Env) GenSelfSigned(cn, certPath, keyPath string, extraSANs ...string) error {
	if e.Exists(certPath) {
		return nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	var ipSANs []net.IP
	var dnsSANs = []string{cn}
	for _, s := range extraSANs {
		if ip := net.ParseIP(s); ip != nil {
			ipSANs = append(ipSANs, ip)
		} else if s != "" {
			dnsSANs = append(dnsSANs, s)
		}
	}
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     dnsSANs,
		IPAddresses:  ipSANs,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	if err := e.WriteFile(certPath, pemBytes("CERTIFICATE", der), 0o644); err != nil {
		return err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	return e.WriteFile(keyPath, pemBytes("EC PRIVATE KEY", keyDER), 0o600)
}

// CertFingerprint returns the base64(SHA256(DER)) fingerprint of the PEM cert
// at certPath. Compatible with the pinSHA256 parameter in hysteria2/TUIC URIs.
func (e *Env) CertFingerprint(certPath string) (string, error) {
	b, err := e.ReadFile(certPath)
	if err != nil {
		return "", err
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return "", fmt.Errorf("no PEM block in %s", certPath)
	}
	cert, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(cert.Raw)
	return base64.StdEncoding.EncodeToString(sum[:]), nil
}

func pemBytes(typ string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
}

// CertPEM returns the PEM certificate at certPath as a string, or "" on error.
func (e *Env) CertPEM(certPath string) string {
	b, err := e.ReadFile(certPath)
	if err != nil {
		return ""
	}
	return string(b)
}

// CertHasIPSAN reports whether the PEM cert at certPath includes ip as a SAN.
// Returns false on any read/parse error so the caller can regenerate safely.
func (e *Env) CertHasIPSAN(certPath, ip string) bool {
	target := net.ParseIP(ip)
	if target == nil {
		return true // not an IP; no SAN needed
	}
	b, err := e.ReadFile(certPath)
	if err != nil {
		return false
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return false
	}
	cert, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		return false
	}
	for _, san := range cert.IPAddresses {
		if san.Equal(target) {
			return true
		}
	}
	return false
}
