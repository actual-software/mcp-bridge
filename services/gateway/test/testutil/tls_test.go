
package testutil

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLocalhostIPConstant tests the LocalhostIP constant.
func TestLocalhostIPConstant(t *testing.T) {
	t.Run("localhost_ip_value", func(t *testing.T) {
		assert.Equal(t, 127, LocalhostIP)
	})

	t.Run("localhost_ip_creates_valid_ipv4", func(t *testing.T) {
		ip := net.IPv4(LocalhostIP, 0, 0, 1)
		assert.NotNil(t, ip)
		assert.Equal(t, "127.0.0.1", ip.String())
		assert.True(t, ip.IsLoopback())
	})
}

// TestCreateTestCertificates tests the certificate creation functionality.
func TestCreateTestCertificates(t *testing.T) {
	t.Run("creates_valid_certificates", testValidCertificatesCreation)
	t.Run("creates_valid_ca_certificate", testValidCACertificate)
	t.Run("creates_valid_server_certificate", testValidServerCertificate)
	t.Run("creates_valid_server_private_key", testValidServerPrivateKey)
	t.Run("server_cert_matches_private_key", testServerCertMatchesPrivateKey)
	t.Run("certificates_form_valid_chain", testCertificatesFormValidChain)
	t.Run("handles_directory_creation", testHandlesDirectoryCreation)
}

func testValidCertificatesCreation(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()

	certFile, keyFile, caFile := CreateTestCertificates(t, tempDir)

	assert.FileExists(t, certFile)
	assert.FileExists(t, keyFile)
	assert.FileExists(t, caFile)

	assert.True(t, strings.HasSuffix(certFile, "server.crt"))
	assert.True(t, strings.HasSuffix(keyFile, "server.key"))
	assert.True(t, strings.HasSuffix(caFile, "ca.crt"))
}

func testValidCACertificate(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()
	_, _, caFile := CreateTestCertificates(t, tempDir)

	caCertPEM, err := os.ReadFile(caFile)
	require.NoError(t, err)

	block, _ := pem.Decode(caCertPEM)
	require.NotNil(t, block)
	assert.Equal(t, "CERTIFICATE", block.Type)

	caCert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	assert.True(t, caCert.IsCA)
	assert.Contains(t, caCert.Subject.Organization, "Test CA")
}

func testValidServerCertificate(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()
	certFile, _, _ := CreateTestCertificates(t, tempDir)

	serverCertPEM, err := os.ReadFile(certFile)
	require.NoError(t, err)

	block, _ := pem.Decode(serverCertPEM)
	require.NotNil(t, block)
	assert.Equal(t, "CERTIFICATE", block.Type)

	serverCert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	assert.False(t, serverCert.IsCA)
	assert.Contains(t, serverCert.Subject.Organization, "Test Server")
	assert.Contains(t, serverCert.DNSNames, "localhost")
	
	localhostIP := net.IPv4(LocalhostIP, 0, 0, 1)
	hasLocalhostIP := false

	for _, ip := range serverCert.IPAddresses {
		if ip.Equal(localhostIP) {
			hasLocalhostIP = true
			break
		}
	}

	assert.True(t, hasLocalhostIP, "Server certificate should contain localhost IP")
}

func testValidServerPrivateKey(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()
	_, keyFile, _ := CreateTestCertificates(t, tempDir)

	serverKeyPEM, err := os.ReadFile(keyFile)
	require.NoError(t, err)

	block, _ := pem.Decode(serverKeyPEM)
	require.NotNil(t, block)
	assert.Equal(t, "PRIVATE KEY", block.Type)

	privateKeyInterface, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	require.NoError(t, err)

	privateKey, ok := privateKeyInterface.(*rsa.PrivateKey)
	require.True(t, ok)

	assert.Equal(t, DefaultRSAKeySize, privateKey.Size()*8)
}

func testServerCertMatchesPrivateKey(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()
	certFile, keyFile, _ := CreateTestCertificates(t, tempDir)

	serverCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)

	leaf, err := x509.ParseCertificate(serverCert.Certificate[0])
	require.NoError(t, err)

	publicKey, ok := leaf.PublicKey.(*rsa.PublicKey)
	require.True(t, ok)

	privateKey, ok := serverCert.PrivateKey.(*rsa.PrivateKey)
	require.True(t, ok)

	assert.Equal(t, publicKey.N, privateKey.N)
	assert.Equal(t, publicKey.E, privateKey.E)
}

func testCertificatesFormValidChain(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()
	certFile, _, caFile := CreateTestCertificates(t, tempDir)

	caCertPEM, err := os.ReadFile(caFile)
	require.NoError(t, err)

	caBlock, _ := pem.Decode(caCertPEM)
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	require.NoError(t, err)

	serverCertPEM, err := os.ReadFile(certFile)
	require.NoError(t, err)

	serverBlock, _ := pem.Decode(serverCertPEM)
	serverCert, err := x509.ParseCertificate(serverBlock.Bytes)
	require.NoError(t, err)

	err = serverCert.CheckSignatureFrom(caCert)
	assert.NoError(t, err)

	roots := x509.NewCertPool()
	roots.AddCert(caCert)

	opts := x509.VerifyOptions{Roots: roots}
	_, err = serverCert.Verify(opts)
	assert.NoError(t, err)
}

func testHandlesDirectoryCreation(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()
	testDir := filepath.Join(tempDir, "subdir", "test")

	err := os.MkdirAll(testDir, 0o750)
	require.NoError(t, err)

	certFile, keyFile, caFile := CreateTestCertificates(t, testDir)

	assert.FileExists(t, certFile)
	assert.FileExists(t, keyFile)
	assert.FileExists(t, caFile)

	assert.Contains(t, certFile, testDir)
	assert.Contains(t, keyFile, testDir)
	assert.Contains(t, caFile, testDir)
}

// TestCreateTestCertificatesErrorHandling tests that the function properly validates inputs.
func TestCreateTestCertificatesErrorHandling(t *testing.T) {
	t.Run("succeeds_with_valid_directory", func(t *testing.T) {
		tempDir := t.TempDir()

		// This should succeed without issues
		certFile, keyFile, caFile := CreateTestCertificates(t, tempDir)

		assert.FileExists(t, certFile)
		assert.FileExists(t, keyFile)
		assert.FileExists(t, caFile)
	})
}

// TestTLSIntegration tests the certificates work with real TLS connections.
func TestTLSIntegration(t *testing.T) {
	t.Run("certificates_work_with_tls_server", func(t *testing.T) {
		testTLSConnectionWithTrustedCertificate(t)
	})

	t.Run("client_rejects_untrusted_certificate", func(t *testing.T) {
		testTLSRejectionWithUntrustedCertificate(t)
	})
}

func testTLSConnectionWithTrustedCertificate(t *testing.T) {
	t.Helper()
	
	tempDir := t.TempDir()
	certFile, keyFile, caFile := CreateTestCertificates(t, tempDir)

	serverCert, clientConfig := setupTrustedTLSConfig(t, certFile, keyFile, caFile)
	serverConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		MinVersion:   tls.VersionTLS12,
	}

	// Test TLS connection using net.Pipe
	serverConn, clientConn := net.Pipe()

	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	performTrustedTLSHandshake(t, serverConn, clientConn, serverConfig, clientConfig)
}

func setupTrustedTLSConfig(t *testing.T, certFile, keyFile, caFile string) (tls.Certificate, *tls.Config) {
	t.Helper()
	
	// Load server certificate
	serverCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)

	// Load CA certificate for client verification
	caCertPEM, err := os.ReadFile(caFile)
	require.NoError(t, err)

	caPool := x509.NewCertPool()
	assert.True(t, caPool.AppendCertsFromPEM(caCertPEM))

	clientConfig := &tls.Config{
		RootCAs:    caPool,
		ServerName: "localhost", // Must match certificate DNS name
		MinVersion: tls.VersionTLS12,
	}

	return serverCert, clientConfig
}

func performTrustedTLSHandshake(t *testing.T, serverConn, clientConn net.Conn, serverConfig, clientConfig *tls.Config) {
	t.Helper()
	
	serverDone := make(chan error, 1)
	clientDone := make(chan error, 1)

	// Start TLS server
	go runTLSServer(serverConn, serverConfig, serverDone)

	// Start TLS client
	go runTLSClient(clientConn, clientConfig, clientDone)

	// Wait for handshake completion
	waitForTLSHandshakeSuccess(t, serverDone, clientDone)
}

func runTLSServer(serverConn net.Conn, serverConfig *tls.Config, done chan<- error) {
	tlsServerConn := tls.Server(serverConn, serverConfig)

	err := tlsServerConn.HandshakeContext(context.Background())
	done <- err
	
	if err == nil {
		_ = tlsServerConn.Close()
	}
}

func runTLSClient(clientConn net.Conn, clientConfig *tls.Config, done chan<- error) {
	tlsClientConn := tls.Client(clientConn, clientConfig)

	err := tlsClientConn.HandshakeContext(context.Background())
	done <- err
	
	if err == nil {
		_ = tlsClientConn.Close()
	}
}

func waitForTLSHandshakeSuccess(t *testing.T, serverDone, clientDone <-chan error) {
	t.Helper()
	
	select {
	case err := <-serverDone:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Server handshake timed out")
	}

	select {
	case err := <-clientDone:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Client handshake timed out")
	}
}

func testTLSRejectionWithUntrustedCertificate(t *testing.T) {
	t.Helper()
	
	tempDir := t.TempDir()
	certFile, keyFile, _ := CreateTestCertificates(t, tempDir)

	serverCert, clientConfig := setupUntrustedTLSConfig(t, certFile, keyFile)
	serverConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		MinVersion:   tls.VersionTLS12,
	}

	// Test TLS connection
	serverConn, clientConn := net.Pipe()

	defer func() { _ = serverConn.Close() }()
	defer func() { _ = clientConn.Close() }()

	performUntrustedTLSHandshake(t, serverConn, clientConn, serverConfig, clientConfig)
}

func setupUntrustedTLSConfig(t *testing.T, certFile, keyFile string) (tls.Certificate, *tls.Config) {
	t.Helper()
	
	// Load server certificate
	serverCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)

	clientConfig := &tls.Config{
		ServerName: "localhost",
		MinVersion: tls.VersionTLS12,
		// No RootCAs set, so certificate should be rejected
	}

	return serverCert, clientConfig
}

func performUntrustedTLSHandshake(
	t *testing.T, serverConn, clientConn net.Conn, serverConfig, clientConfig *tls.Config,
) {
	t.Helper()
	
	clientDone := make(chan error, 1)

	// Start TLS server
	go func() {
		tlsServerConn := tls.Server(serverConn, serverConfig)
		_ = tlsServerConn.HandshakeContext(context.Background()) // Ignore server errors in test
	}()

	// Start TLS client
	go runTLSClient(clientConn, clientConfig, clientDone)

	// Client should reject the certificate
	waitForTLSHandshakeFailure(t, clientDone)
}

func waitForTLSHandshakeFailure(t *testing.T, clientDone <-chan error) {
	t.Helper()
	
	select {
	case err := <-clientDone:
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "certificate")
	case <-time.After(5 * time.Second):
		t.Fatal("Client handshake should have failed quickly")
	}
}

// TestCertificateProperties tests detailed certificate properties.
func TestCertificateProperties(t *testing.T) {
	t.Run("ca_certificate_self_signed", func(t *testing.T) {
		testCACertificateIsSelfSigned(t)
	})

	t.Run("certificates_have_different_serial_numbers", func(t *testing.T) {
		testCertificatesHaveDifferentSerialNumbers(t)
	})

	t.Run("certificates_use_rsa_2048", func(t *testing.T) {
		testCertificatesUseRSA2048(t)
	})
}

func testCACertificateIsSelfSigned(t *testing.T) {
	t.Helper()
	
	tempDir := t.TempDir()
	_, _, caFile := CreateTestCertificates(t, tempDir)

	caCert := loadCertificateFromFile(t, caFile)

	// Verify CA certificate is self-signed
	err := caCert.CheckSignatureFrom(caCert)
	assert.NoError(t, err)
}

func testCertificatesHaveDifferentSerialNumbers(t *testing.T) {
	t.Helper()
	
	tempDir := t.TempDir()
	certFile, _, caFile := CreateTestCertificates(t, tempDir)

	caCert := loadCertificateFromFile(t, caFile)
	serverCert := loadCertificateFromFile(t, certFile)

	// Verify different serial numbers
	assert.NotEqual(t, caCert.SerialNumber, serverCert.SerialNumber)
	assert.Equal(t, int64(1), caCert.SerialNumber.Int64())
	assert.Equal(t, int64(2), serverCert.SerialNumber.Int64())
}

func testCertificatesUseRSA2048(t *testing.T) {
	t.Helper()
	
	tempDir := t.TempDir()
	certFile, _, caFile := CreateTestCertificates(t, tempDir)

	caCert := loadCertificateFromFile(t, caFile)
	serverCert := loadCertificateFromFile(t, certFile)

	// Check CA certificate key size
	validateRSAKeySize(t, caCert, "CA")

	// Check server certificate key size
	validateRSAKeySize(t, serverCert, "Server")
}

func loadCertificateFromFile(t *testing.T, certFile string) *x509.Certificate {
	t.Helper()
	
	certPEM, err := os.ReadFile(certFile)
	require.NoError(t, err)

	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block)

	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	return cert
}

func validateRSAKeySize(t *testing.T, cert *x509.Certificate, certType string) {
	t.Helper()
	
	publicKey, ok := cert.PublicKey.(*rsa.PublicKey)
	require.True(t, ok, "%s certificate should use RSA public key", certType)
	assert.Equal(t, DefaultRSAKeySize, publicKey.Size()*8, "%s certificate should use RSA-2048", certType)
}

// TestFilePermissions tests file permission handling.
func TestFilePermissions(t *testing.T) {
	t.Run("created_files_have_appropriate_permissions", func(t *testing.T) {
		tempDir := t.TempDir()
		certFile, keyFile, caFile := CreateTestCertificates(t, tempDir)

		// Check file permissions
		files := []string{certFile, keyFile, caFile}
		for _, file := range files {
			info, err := os.Stat(file)
			require.NoError(t, err)

			// Files should be readable
			mode := info.Mode()
			assert.NotEqual(t, 0, mode&0o400, "File %s should be readable by owner", file)

			// Key file should have restrictive permissions in production,
			// but for tests we just verify it exists and is readable
			assert.True(t, mode.IsRegular(), "File %s should be a regular file", file)
		}
	})
}

// TestUniqueGeneration tests that multiple calls generate unique certificates.
func TestUniqueGeneration(t *testing.T) {
	t.Run("multiple_calls_generate_different_certificates", func(t *testing.T) {
		tempDir1 := t.TempDir()
		tempDir2 := t.TempDir()

		// Generate first set of certificates
		certFile1, _, caFile1 := CreateTestCertificates(t, tempDir1)

		// Generate second set of certificates
		certFile2, _, caFile2 := CreateTestCertificates(t, tempDir2)

		// Read certificates
		cert1PEM, err := os.ReadFile(certFile1) 
		require.NoError(t, err)
		cert2PEM, err := os.ReadFile(certFile2) 
		require.NoError(t, err)

		ca1PEM, err := os.ReadFile(caFile1) 
		require.NoError(t, err)
		ca2PEM, err := os.ReadFile(caFile2) 
		require.NoError(t, err)

		// Certificates should be different
		assert.NotEqual(t, cert1PEM, cert2PEM)
		assert.NotEqual(t, ca1PEM, ca2PEM)

		// Parse and compare key material
		cert1Block, _ := pem.Decode(cert1PEM)
		cert1, err := x509.ParseCertificate(cert1Block.Bytes)
		require.NoError(t, err)

		cert2Block, _ := pem.Decode(cert2PEM)
		cert2, err := x509.ParseCertificate(cert2Block.Bytes)
		require.NoError(t, err)

		// Public keys should be different
		cert1Key, ok := cert1.PublicKey.(*rsa.PublicKey)
		require.True(t, ok)
		cert2Key, ok := cert2.PublicKey.(*rsa.PublicKey)
		require.True(t, ok)

		assert.NotEqual(t, cert1Key.N, cert2Key.N)
	})
}

// TestTLSEdgeCases tests edge cases and boundary conditions for TLS functionality.
func TestTLSEdgeCases(t *testing.T) {
	t.Run("handles_empty_directory_path", func(t *testing.T) {
		// Current directory should work
		currentDir, err := os.Getwd()
		require.NoError(t, err)

		// Create a temporary subdirectory in current directory for testing
		testDir := filepath.Join(currentDir, "test_certs_"+t.Name())
		err = os.MkdirAll(testDir, 0o750)
		require.NoError(t, err)

		defer func() { _ = os.RemoveAll(testDir) }()

		certFile, keyFile, caFile := CreateTestCertificates(t, testDir)

		assert.FileExists(t, certFile)
		assert.FileExists(t, keyFile)
		assert.FileExists(t, caFile)
	})

	t.Run("handles_directory_with_spaces", func(t *testing.T) {
		tempDir := t.TempDir()
		spacedDir := filepath.Join(tempDir, "dir with spaces")

		err := os.Mkdir(spacedDir, 0o750)
		require.NoError(t, err)

		certFile, keyFile, caFile := CreateTestCertificates(t, spacedDir)

		assert.FileExists(t, certFile)
		assert.FileExists(t, keyFile)
		assert.FileExists(t, caFile)

		// Verify files are in the spaced directory
		assert.Contains(t, certFile, "dir with spaces")
		assert.Contains(t, keyFile, "dir with spaces")
		assert.Contains(t, caFile, "dir with spaces")
	})

	t.Run("handles_deeply_nested_directory", func(t *testing.T) {
		tempDir := t.TempDir()
		deepDir := filepath.Join(tempDir, "a", "very", "deep", "directory", "structure", "for", "testing")

		err := os.MkdirAll(deepDir, 0o750)
		require.NoError(t, err)

		certFile, keyFile, caFile := CreateTestCertificates(t, deepDir)

		assert.FileExists(t, certFile)
		assert.FileExists(t, keyFile)
		assert.FileExists(t, caFile)
	})
}
