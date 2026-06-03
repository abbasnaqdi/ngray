package system

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
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

func pemBytes(typ string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
}
