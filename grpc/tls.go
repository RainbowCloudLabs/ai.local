package grpc

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	"google.golang.org/grpc/credentials"
)

// GRPCKeyPair holds the in-memory TLS configuration payloads.
type GRPCKeyPair struct {
	CertPEM []byte
	KeyPEM  []byte
}

// GenerateEphemeralGRPCTLSConfig creates an in-memory, self-signed TLS credential set for gRPC.
func GenerateEphemeralGRPCTLSConfig() (credentials.TransportCredentials, error) {
	// 1. Generate the underlying RSA private key
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ephemeral rsa key: %w", err)
	}

	// 2. Set up the certificate template with safe defaults
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"ai.local"},
			CommonName:   "127.0.0.1", // Enforces loopback or local admin isolation
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour), // Valid for 1 year
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// 3. Create the self-signed X.509 certificate payload
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("failed to create self-signed certificate: %w", err)
	}

	// 4. Encode certificate and private key to PEM blocks
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal pkcs8 private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})

	// 5. Construct gRPC transport credentials directly from memory bytes
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to parse memory pem keypair: %w", err)
	}
	cert := credentials.NewServerTLSFromCert(&tlsCert)

	return cert, nil
}
