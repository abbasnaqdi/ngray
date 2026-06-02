package system

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
)

// GenReality returns a base64url X25519 keypair compatible with REALITY
// (xray/sing-box/shoes), generated natively without invoking a core binary.
func GenReality() (priv, pub string, err error) {
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString(k.Bytes()), enc.EncodeToString(k.PublicKey().Bytes()), nil
}
