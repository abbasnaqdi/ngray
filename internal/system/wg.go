package system

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
)

// GenWGKeyPair returns a WireGuard-compatible X25519 keypair encoded in
// standard base64 (not base64url — wg-quick requires standard padding).
func GenWGKeyPair() (priv, pub string, err error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	enc := base64.StdEncoding
	return enc.EncodeToString(k.Bytes()), enc.EncodeToString(k.PublicKey().Bytes()), nil
}
