package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"
)

const (
	testCertTimeout = 30 * time.Second
)

func main() {
	// Load CA certificate
	caCert, err := os.ReadFile("./certs/ca.crt")
	if err != nil {
		fmt.Printf("Failed to read CA certificate: %v\n", err)
		os.Exit(1)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		fmt.Printf("Failed to parse CA certificate\n")
		os.Exit(1)
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    caCertPool,
				MinVersion: tls.VersionTLS12,
			},
		},
		Timeout: testCertTimeout,
	}

	ctx := context.Background()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://localhost:8443/health", nil)
	if err != nil {
		fmt.Printf("Failed to create request: %v\n", err)
		os.Exit(1)
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Request failed: %v\n", err)
		os.Exit(1)
	}

	defer func() { _ = resp.Body.Close() }()

	fmt.Printf("Success! Status: %d\n", resp.StatusCode)
}
