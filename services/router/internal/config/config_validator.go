package config

import (
	"errors"
	"fmt"
)

// ConfigValidator handles configuration validation.
type ConfigValidator struct {
	config *Config
}

// CreateConfigValidator creates a new configuration validator.
func CreateConfigValidator(cfg *Config) *ConfigValidator {
	return &ConfigValidator{config: cfg}
}

// ValidateConfiguration performs comprehensive config validation.
func (v *ConfigValidator) ValidateConfiguration() error {
	if err := v.validateEndpoints(); err != nil {
		return err
	}

	return v.applyEndpointDefaults()
}

// validateEndpoints ensures endpoints are properly configured.
func (v *ConfigValidator) validateEndpoints() error {
	if len(v.config.GatewayPool.Endpoints) == 0 {
		return errors.New("gateway_pool.endpoints is required - at least one endpoint must be configured")
	}

	for i, endpoint := range v.config.GatewayPool.Endpoints {
		if err := v.validateEndpoint(i, &endpoint); err != nil {
			return err
		}
	}

	return nil
}

// validateEndpoint validates a single endpoint configuration.
func (v *ConfigValidator) validateEndpoint(index int, endpoint *GatewayEndpoint) error {
	if endpoint.URL == "" {
		return fmt.Errorf("gateway_pool.endpoints[%d].url is required", index)
	}

	return v.validateEndpointAuth(index, endpoint)
}

// validateEndpointAuth validates authentication configuration.
func (v *ConfigValidator) validateEndpointAuth(index int, endpoint *GatewayEndpoint) error {
	if endpoint.Auth.Type == "" {
		v.config.GatewayPool.Endpoints[index].Auth.Type = AuthTypeBearerToken
		endpoint.Auth.Type = AuthTypeBearerToken
	}

	switch endpoint.Auth.Type {
	case AuthTypeBearerToken:
		// Token will be loaded separately.
		return nil
	case AuthTypeMTLS:
		return v.validateMTLSAuth(index, endpoint)
	case AuthTypeOAuth2:
		return v.validateOAuth2Auth(index, endpoint)
	default:
		return fmt.Errorf("unsupported auth type '%s' on endpoint %d", endpoint.Auth.Type, index)
	}
}

// validateMTLSAuth validates mTLS authentication requirements.
func (v *ConfigValidator) validateMTLSAuth(index int, endpoint *GatewayEndpoint) error {
	if endpoint.Auth.ClientCert == "" || endpoint.Auth.ClientKey == "" {
		return fmt.Errorf("client_cert and client_key are required for mtls auth on endpoint %d", index)
	}

	return nil
}

// validateOAuth2Auth validates OAuth2 authentication requirements.
func (v *ConfigValidator) validateOAuth2Auth(index int, endpoint *GatewayEndpoint) error {
	if endpoint.Auth.ClientID == "" || endpoint.Auth.TokenEndpoint == "" {
		return fmt.Errorf("client_id and token_endpoint are required for oauth2 auth on endpoint %d", index)
	}

	return nil
}

// applyEndpointDefaults sets default values for endpoints.
func (v *ConfigValidator) applyEndpointDefaults() error {
	for i := range v.config.GatewayPool.Endpoints {
		v.setEndpointDefaults(i)
	}

	return nil
}

// setEndpointDefaults applies defaults to a single endpoint.
func (v *ConfigValidator) setEndpointDefaults(index int) {
	endpoint := &v.config.GatewayPool.Endpoints[index]

	// Set default weight.
	if endpoint.Weight <= 0 {
		endpoint.Weight = 1
	}

	// Set default priority.
	if endpoint.Priority == 0 {
		endpoint.Priority = 1
	}

	// Set default tags.
	if len(endpoint.Tags) == 0 {
		endpoint.Tags = []string{"default"}
	}
}

// validate is the main validation entry point (replaces old function).
func validate(cfg *Config) error {
	validator := CreateConfigValidator(cfg)

	return validator.ValidateConfiguration()
}
