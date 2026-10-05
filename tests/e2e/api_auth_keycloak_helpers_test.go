//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

type keycloakAdminClient struct {
	adminURL    string
	adminToken  string
	tokenIssuer string
	adminUser   string
	adminPass   string
	username    string
	password    string
	client      *http.Client
}

type keycloakClientRepresentation struct {
	ID       string `json:"id"`
	ClientID string `json:"clientId"`
}

type keycloakComponent struct {
	ID     string              `json:"id"`
	Name   string              `json:"name"`
	Config map[string][]string `json:"config"`
}

func loadKeycloakAdminClient() (*keycloakAdminClient, error) {
	adminURL := strings.TrimRight(os.Getenv("DCM_AUTH_ADMIN_URL"), "/")
	adminToken := os.Getenv("DCM_AUTH_ADMIN_TOKEN")
	if adminURL == "" || adminToken == "" {
		return nil, fmt.Errorf("DCM_AUTH_ADMIN_URL and DCM_AUTH_ADMIN_TOKEN are required")
	}
	adminUser := os.Getenv("DCM_AUTH_ADMIN_USERNAME")
	adminPass := os.Getenv("DCM_AUTH_ADMIN_PASSWORD")
	if (adminUser == "") != (adminPass == "") {
		return nil, fmt.Errorf("DCM_AUTH_ADMIN_USERNAME and DCM_AUTH_ADMIN_PASSWORD must be provided together")
	}
	tokenIssuer := authTokens.settings.tokenIssuerURL
	if tokenIssuer == "" {
		tokenIssuer = authTokens.settings.issuerURL
	}
	if tokenIssuer == "" || authTokens.settings.username == "" || authTokens.settings.password == "" {
		return nil, fmt.Errorf("auth token issuer, username, and password are required for Keycloak E2E helpers")
	}
	return &keycloakAdminClient{
		adminURL:    adminURL,
		adminToken:  adminToken,
		tokenIssuer: strings.TrimRight(tokenIssuer, "/"),
		adminUser:   adminUser,
		adminPass:   adminPass,
		username:    authTokens.settings.username,
		password:    authTokens.settings.password,
		client:      unauthenticatedClient,
	}, nil
}

func (k *keycloakAdminClient) request(ctx context.Context, method, path string, payload, result interface{}) (*http.Response, error) {
	var bodyBytes []byte
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode Keycloak request: %w", err)
		}
		bodyBytes = data
	}
	doRequest := func(token string) (*http.Response, error) {
		var body io.Reader
		if bodyBytes != nil {
			body = bytes.NewReader(bodyBytes)
		}
		request, err := http.NewRequestWithContext(ctx, method, k.adminURL+path, body)
		if err != nil {
			return nil, fmt.Errorf("create Keycloak request: %w", err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		if payload != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		response, err := k.client.Do(request)
		if err != nil {
			return nil, fmt.Errorf("send Keycloak request: %w", err)
		}
		return response, nil
	}
	response, err := doRequest(k.adminToken)
	if err != nil {
		return nil, err
	}
	if response.StatusCode == http.StatusUnauthorized && k.adminUser != "" && k.adminPass != "" {
		response.Body.Close()
		freshToken, refreshErr := k.issueAdminToken(ctx)
		if refreshErr == nil {
			k.adminToken = freshToken
			response, err = doRequest(k.adminToken)
			if err != nil {
				return nil, err
			}
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("Keycloak %s %s returned HTTP %d: %s", method, path, response.StatusCode, strings.TrimSpace(string(message)))
	}
	if result != nil {
		defer response.Body.Close()
		if err := json.NewDecoder(response.Body).Decode(result); err != nil {
			return nil, fmt.Errorf("decode Keycloak response: %w", err)
		}
	}
	return response, nil
}

func (k *keycloakAdminClient) issueAdminToken(ctx context.Context) (string, error) {
	issuerBase := k.tokenIssuer
	if marker := strings.Index(issuerBase, "/realms/"); marker >= 0 {
		issuerBase = issuerBase[:marker]
	}
	return k.issuePasswordToken(ctx,
		issuerBase+"/realms/master/protocol/openid-connect/token",
		"admin-cli", "", k.adminUser, k.adminPass)
}

func (k *keycloakAdminClient) issuePasswordToken(ctx context.Context, tokenURL, clientID, clientSecret, username, password string) (string, error) {
	config := oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       []string{"openid"},
		Endpoint: oauth2.Endpoint{
			TokenURL:  tokenURL,
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
	tokenContext := context.WithValue(ctx, oauth2.HTTPClient, k.client)
	token, err := config.PasswordCredentialsToken(tokenContext, username, password)
	if err != nil {
		return "", fmt.Errorf("request Keycloak token: %w", err)
	}
	if token.AccessToken == "" {
		return "", fmt.Errorf("Keycloak token response was empty")
	}
	return token.AccessToken, nil
}

func (k *keycloakAdminClient) createPublicClient(ctx context.Context, lifespan int) (keycloakClientRepresentation, error) {
	clientID := fmt.Sprintf("dcm-e2e-%d", time.Now().UnixNano())
	payload := map[string]interface{}{
		"clientId": clientID, "enabled": true, "protocol": "openid-connect",
		"publicClient": true, "standardFlowEnabled": false, "directAccessGrantsEnabled": true,
		"attributes": map[string]string{"access.token.lifespan": strconv.Itoa(lifespan)},
	}
	response, err := k.request(ctx, http.MethodPost, "/clients", payload, nil)
	if err != nil {
		return keycloakClientRepresentation{}, err
	}
	response.Body.Close()
	var clients []keycloakClientRepresentation
	_, err = k.request(ctx, http.MethodGet, "/clients?clientId="+url.QueryEscape(clientID), nil, &clients)
	if err != nil {
		return keycloakClientRepresentation{}, err
	}
	if len(clients) != 1 || clients[0].ID == "" {
		return keycloakClientRepresentation{}, fmt.Errorf("temporary Keycloak client %q was not found", clientID)
	}
	return clients[0], nil
}

func (k *keycloakAdminClient) addAudienceMapper(ctx context.Context, clientID, audience string) error {
	payload := map[string]interface{}{
		"name": "dcm-e2e-audience", "protocol": "openid-connect",
		"protocolMapper": "oidc-audience-mapper",
		"config": map[string]string{
			"included.custom.audience": audience,
			"access.token.claim":       "true",
			"id.token.claim":           "false",
		},
	}
	response, err := k.request(ctx, http.MethodPost, "/clients/"+clientID+"/protocol-mappers/models", payload, nil)
	if response != nil {
		response.Body.Close()
	}
	return err
}

func (k *keycloakAdminClient) issueUserToken(ctx context.Context, clientID string) (string, error) {
	return k.issuePasswordToken(ctx,
		k.tokenIssuer+"/protocol/openid-connect/token",
		clientID, "", k.username, k.password)
}

func (k *keycloakAdminClient) delete(ctx context.Context, path string) error {
	response, err := k.request(ctx, http.MethodDelete, path, nil, nil)
	if response != nil {
		response.Body.Close()
	}
	return err
}

func (k *keycloakAdminClient) realmID(ctx context.Context) (string, error) {
	var realm struct {
		ID string `json:"id"`
	}
	_, err := k.request(ctx, http.MethodGet, "", nil, &realm)
	if err != nil {
		return "", err
	}
	if realm.ID == "" {
		return "", fmt.Errorf("Keycloak realm response did not contain id")
	}
	return realm.ID, nil
}

func (k *keycloakAdminClient) listClients(ctx context.Context) error {
	var clients []keycloakClientRepresentation
	_, err := k.request(ctx, http.MethodGet, "/clients?max=1", nil, &clients)
	return err
}

func (k *keycloakAdminClient) signingComponents(ctx context.Context) ([]keycloakComponent, error) {
	var components []keycloakComponent
	_, err := k.request(ctx, http.MethodGet, "/components?type=org.keycloak.keys.KeyProvider", nil, &components)
	return components, err
}

func maxSigningPriority(components []keycloakComponent) int {
	maximum := 0
	for _, component := range components {
		if len(component.Config["priority"]) == 0 {
			continue
		}
		priority, err := strconv.Atoi(component.Config["priority"][0])
		if err == nil && priority > maximum {
			maximum = priority
		}
	}
	return maximum
}

func (k *keycloakAdminClient) createSigningProvider(ctx context.Context) (string, error) {
	realmID, err := k.realmID(ctx)
	if err != nil {
		return "", err
	}
	components, err := k.signingComponents(ctx)
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("dcm-e2e-rsa-%d", time.Now().UnixNano())
	priority := strconv.Itoa(maxSigningPriority(components) + 100)
	payload := map[string]interface{}{
		"name": name, "parentId": realmID, "providerId": "rsa-generated",
		"providerType": "org.keycloak.keys.KeyProvider",
		"config": map[string][]string{
			"priority": {priority}, "algorithm": {"RS256"}, "keySize": {"2048"},
			"enabled": {"true"}, "active": {"true"},
		},
	}
	response, err := k.request(ctx, http.MethodPost, "/components", payload, nil)
	if err != nil {
		return "", err
	}
	location := response.Header.Get("Location")
	response.Body.Close()
	createdID := path.Base(location)
	components, err = k.signingComponents(ctx)
	if err != nil {
		if createdID != "" && createdID != "." && createdID != "/" {
			_ = k.delete(ctx, "/components/"+createdID)
		}
		return "", err
	}
	for _, component := range components {
		if component.Name == name {
			return component.ID, nil
		}
	}
	if createdID != "" && createdID != "." && createdID != "/" {
		_ = k.delete(ctx, "/components/"+createdID)
	}
	return "", fmt.Errorf("temporary signing provider %q was not found", name)
}
