package receiver_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
)

func genPrivateKey() (*rsa.PrivateKey, error) {
	const bits = 2048
	priv, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return nil, err
	}
	if err := priv.Validate(); err != nil {
		return nil, err
	}
	return priv, nil
}

func asPEM(priv *rsa.PrivateKey) []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:    "RSA PRIVATE KEY",
		Headers: nil,
		Bytes:   x509.MarshalPKCS1PrivateKey(priv),
	})
}

func genKey(privKeyPath string) error {
	priv, err := genPrivateKey()
	if err != nil {
		return err
	}
	if err := os.WriteFile(privKeyPath, asPEM(priv), 0600); err != nil {
		return err
	}
	return nil
}
