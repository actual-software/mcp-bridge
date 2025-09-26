package config

import (
	"errors"
	"fmt"
)

// BuildComplexNamespaceRoutingScenario creates a complex namespace routing scenario.
func BuildComplexNamespaceRoutingScenario() SecurityTestScenario {
	return SecurityTestScenario{
		Name: "Complex namespace routing with security",
		ConfigYAML: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://docker-secure.com
      auth:
        type: bearer
        token_env: DOCKER_TOKEN
      tags: ["docker", "secure"]
      weight: 10
      priority: 1
    - url: wss://k8s-secure.com
      auth:
        type: mtls
        client_cert: %s
        client_key: %s
      tags: ["k8s", "secure"]
      weight: 8
      priority: 2
    - url: wss://general-secure.com
      auth:
        type: oauth2
        client_id: general-client
        client_secret_env: GENERAL_SECRET
        token_endpoint: https://auth.example.com/token
      tags: ["general"]
      weight: 5
      priority: 3
  namespace_routing:
    enabled: true
    rules:
      - pattern: "^docker\\.(images|containers|networks)\\."
        tags: ["docker"]
        priority: 1
        description: "Route Docker API calls to Docker endpoint"
      - pattern: "^k8s\\.(pods|services|deployments)\\."
        tags: ["k8s"]
        priority: 2
        description: "Route Kubernetes API calls to K8s endpoint"
      - pattern: ".*"
        tags: ["general"]
        priority: 10
        description: "Default routing for other calls"
`,
		EnvVars: map[string]string{
			"DOCKER_TOKEN":   "docker-secure-token",
			"GENERAL_SECRET": "general-oauth-secret",
		},
		FileTokens: map[string]string{
			"k8s-client.crt": "-----BEGIN CERTIFICATE-----\nK8S_CERT_DATA\n-----END CERTIFICATE-----",
			"k8s-client.key": "-----BEGIN PRIVATE KEY-----\nK8S_KEY_DATA\n-----END PRIVATE KEY-----",
		},
		Validate: validateComplexNamespaceRouting,
	}
}

func validateComplexNamespaceRouting(c *Config) error {
	const expectedEndpointCount = 3

	const expectedRuleCount = 3

	endpoints := c.GetGatewayEndpoints()
	if len(endpoints) != expectedEndpointCount {
		return fmt.Errorf("expected %d endpoints, got %d", expectedEndpointCount, len(endpoints))
	}

	nsConfig := c.GetNamespaceRoutingConfig()
	if !nsConfig.Enabled {
		return errors.New("namespace routing should be enabled")
	}

	if len(nsConfig.Rules) != expectedRuleCount {
		return fmt.Errorf("expected %d routing rules, got %d", expectedRuleCount, len(nsConfig.Rules))
	}

	// Verify endpoints have correct auth.
	if err := validateDockerEndpoint(endpoints[0]); err != nil {
		return err
	}

	if err := validateK8sEndpoint(endpoints[1]); err != nil {
		return err
	}

	if err := validateGeneralEndpoint(endpoints[2]); err != nil {
		return err
	}

	return nil
}

func validateDockerEndpoint(endpoint GatewayEndpoint) error {
	if endpoint.Auth.Token != "docker-secure-token" {
		return errors.New("docker endpoint token not correct")
	}

	return nil
}

func validateK8sEndpoint(endpoint GatewayEndpoint) error {
	if endpoint.Auth.Type != "mtls" {
		return errors.New("k8s endpoint should use mTLS")
	}

	return nil
}

func validateGeneralEndpoint(endpoint GatewayEndpoint) error {
	if endpoint.Auth.ClientSecret != "general-oauth-secret" {
		return errors.New("general endpoint oauth secret not correct")
	}

	return nil
}
