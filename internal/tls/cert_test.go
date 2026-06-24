package tls

import (
	"crypto/tls"
	"path/filepath"
	"testing"
)

func TestExtractHost(t *testing.T) {
	tests := []struct {
		name    string
		baseURI string
		want    string
		wantErr bool
	}{
		{"Standard HTTPS", "https://ai.local", "ai.local", false},
		{"Bare hostname", "ai.local", "ai.local", false},
		{"With Port", "https://ai.local:443", "ai.local", false},
		{"IP Address", "https://192.168.1.2", "192.168.1.2", false},
		{"IP with Port", "192.168.1.2:8443", "192.168.1.2", false},
		{"Empty URI", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractHost(tt.baseURI)

			if (err != nil) != tt.wantErr {
				t.Fatalf("extractHost() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("extractHost() got = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestGenerateSelfSignedCert
func TestGenerateSelfSignedCert(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "ai.local.crt")
	keyPath := filepath.Join(tmpDir, "ai.local.key")

	baseURI := "https://ai.local"

	if CertExists(certPath, keyPath) {
		t.Fatal("expected cert and key to not exist before generation")
	}

	err := GenerateSelfSignedCert(baseURI, certPath, keyPath)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert() failed: %v", err)
	}

	if !CertExists(certPath, keyPath) {
		t.Fatal("expected cert and key files to exist on disk after generation")
	}

	_, err = tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Errorf("generated TLS keypair is invalid or mismatched: %v", err)
	}
}
