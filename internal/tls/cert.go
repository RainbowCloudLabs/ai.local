package tls

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

// GenerateSelfSignedCert parses the domain from baseURI and generates
// a self-signed TLS certificate and private key, writing them to disk.
func GenerateSelfSignedCert(baseURI, certPath, keyPath string) error {
	host, err := extractHost(baseURI)
	if err != nil {
		return err
	}

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate rsa private key: %w", err)
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"ai.local Edge Proxy"},
			CommonName:   host,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// inject SAN: IP or DNS
	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = append(template.IPAddresses, ip)
	} else {
		template.DNSNames = append(template.DNSNames, host)
	}

	derBytes, err := x509.CreateCertificate(
		rand.Reader,
		&template,
		&template,
		&priv.PublicKey,
		priv,
	)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	if err := writeCert(certPath, derBytes); err != nil {
		return err
	}
	if err := writeKey(keyPath, priv); err != nil {
		return err
	}

	return nil
}

// CertExists checks whether both cert and key files already exist on disk.
func CertExists(certPath, keyPath string) bool {
	_, certErr := os.Stat(certPath)
	_, keyErr := os.Stat(keyPath)
	return certErr == nil && keyErr == nil
}

func extractHost(baseURI string) (string, error) {
	if !strings.Contains(baseURI, "://") {
		baseURI = "https://" + baseURI
	}
	u, err := url.Parse(baseURI)
	if err != nil {
		return "", fmt.Errorf("invalid baseUri %q: %w", baseURI, err)
	}

	host := u.Host
	if host == "" {
		// handle bare hostname without scheme e.g. "ai.local"
		host = u.Path
	}
	if host == "" {
		return "", fmt.Errorf("invalid baseUri %q: cannot extract host", baseURI)
	}

	// strip port if present e.g. "ai.local:443" -> "ai.local"
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	return host, nil
}

func writeCert(certPath string, derBytes []byte) error {
	f, err := os.Create(certPath)
	if err != nil {
		return fmt.Errorf("failed to create cert file %s: %w", certPath, err)
	}
	defer f.Close()

	if err := pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return fmt.Errorf("failed to write cert pem %s: %w", certPath, err)
	}
	return nil
}

func writeKey(keyPath string, priv *rsa.PrivateKey) error {
	f, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create key file %s: %w", keyPath, err)
	}
	defer f.Close()

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}
	if err := pem.Encode(f, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}); err != nil {
		return fmt.Errorf("failed to write key pem %s: %w", keyPath, err)
	}
	return nil
}
