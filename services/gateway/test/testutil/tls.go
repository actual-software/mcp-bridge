package testutil

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	// LocalhostIP is the localhost IP address.
	LocalhostIP = 127

	// Certificate serial numbers for testing.
	serverCertSerialNumber = 2
)

// CreateTestCertificates creates test certificates for TLS testing.
func CreateTestCertificates(t *testing.T, dir string) (certFile, keyFile, caFile string) {
	t.Helper()

	// Create CA
	caKey, caCertDER := createTestCA(t)
	caFile = writeCertToFile(t, dir, "ca.crt", caCertDER)

	// Create server certificate
	serverKey, serverCertDER := createTestServerCert(t, caCertDER, caKey)
	certFile = writeCertToFile(t, dir, "server.crt", serverCertDER)

	// Write server key
	keyFile = writeKeyToFile(t, dir, "server.key", serverKey)

	return certFile, keyFile, caFile
}

// createTestCA creates a test CA certificate and key.
func createTestCA(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()

	caKey, err := rsa.GenerateKey(rand.Reader, DefaultRSAKeySize)
	if err != nil {
		t.Fatalf("Failed to generate CA key: %v", err)
	}

	caTemplate := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test CA"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("Failed to create CA certificate: %v", err)
	}

	return caKey, caCertDER
}

// createTestServerCert creates a test server certificate.
func createTestServerCert(t *testing.T, caCertDER []byte, caKey *rsa.PrivateKey) (*rsa.PrivateKey, []byte) {
	t.Helper()

	serverKey, err := rsa.GenerateKey(rand.Reader, DefaultRSAKeySize)
	if err != nil {
		t.Fatalf("Failed to generate server key: %v", err)
	}

	serverTemplate := x509.Certificate{
		SerialNumber: big.NewInt(serverCertSerialNumber),
		Subject: pkix.Name{
			Organization: []string{"Test Server"},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{"localhost", "test.example.com"},
		IPAddresses: []net.IP{net.IPv4(LocalhostIP, 0, 0, 1)},
	}

	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		t.Fatalf("Failed to parse CA certificate: %v", err)
	}

	serverCertDER, err := x509.CreateCertificate(rand.Reader, &serverTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("Failed to create server certificate: %v", err)
	}

	return serverKey, serverCertDER
}

// writeCertToFile writes a certificate to a PEM file.
func writeCertToFile(t *testing.T, dir, filename string, certDER []byte) string {
	t.Helper()

	filePath := filepath.Join(dir, filename)

	certOut, err := os.Create(filePath) // #nosec G304 - Test file creation in controlled environment
	if err != nil {
		t.Fatalf("Failed to open %s for writing: %v", filename, err)
	}

	defer func() {
		if err := certOut.Close(); err != nil {
			t.Logf("Failed to close %s: %v", filename, err)
		}
	}()

	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		t.Fatalf("Failed to write certificate to %s: %v", filename, err)
	}

	return filePath
}

// writeKeyToFile writes a private key to a PEM file.
func writeKeyToFile(t *testing.T, dir, filename string, key *rsa.PrivateKey) string {
	t.Helper()

	filePath := filepath.Join(dir, filename)

	keyOut, err := os.Create(filePath) // #nosec G304 - Test file creation in controlled environment
	if err != nil {
		t.Fatalf("Failed to open %s for writing: %v", filename, err)
	}

	defer func() {
		if err := keyOut.Close(); err != nil {
			t.Logf("Failed to close %s: %v", filename, err)
		}
	}()

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("Failed to marshal private key: %v", err)
	}

	if err := pem.Encode(keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}); err != nil {
		t.Fatalf("Failed to write key to %s: %v", filename, err)
	}

	return filePath
}
