//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const defaultStorageRegisteredEndpoint = "embedded://storage"

// agentProvider matches environment-agent GET /providers (OpenAPI Provider).
type agentProvider struct {
	ID          string `json:"id"`
	ServiceType string `json:"service_type"`
	Endpoint    string `json:"endpoint"`
	Type        string `json:"type"`
	Status      string `json:"status"`
}

type agentProviderList struct {
	Results       []agentProvider `json:"results"`
	NextPageToken string          `json:"next_page_token"`
}

var (
	storageSPRegisteredEndpoint string
	environmentAgentBaseURL     string
	environmentAgentReady       bool
)

func initEnvironmentAgent() {
	environmentAgentBaseURL = os.Getenv("DCM_ENVIRONMENT_AGENT_URL")
	if environmentAgentBaseURL == "" {
		environmentAgentBaseURL = os.Getenv("DCM_AGENT_URL")
	}
	if environmentAgentBaseURL == "" {
		GinkgoWriter.Println("DCM_ENVIRONMENT_AGENT_URL/DCM_AGENT_URL unset — storage registration tests will be skipped")
		return
	}
	environmentAgentBaseURL = strings.TrimRight(environmentAgentBaseURL, "/")

	storageSPRegisteredEndpoint = os.Getenv("K8S_STORAGE_SP_REGISTERED_ENDPOINT")
	if storageSPRegisteredEndpoint == "" {
		storageSPRegisteredEndpoint = defaultStorageRegisteredEndpoint
	}

	resp, err := httpClient.Get(environmentAgentBaseURL + "/health")
	if err != nil {
		GinkgoWriter.Printf("Environment agent not reachable at %s: %v — storage registration tests will be skipped\n", environmentAgentBaseURL, err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		GinkgoWriter.Printf("Environment agent health returned %d — storage registration tests will be skipped\n", resp.StatusCode)
		return
	}
	environmentAgentReady = true
	GinkgoWriter.Printf("Environment agent ready at %s\n", environmentAgentBaseURL)
	providers, err := fetchEnvironmentAgentProviders()
	if err != nil {
		GinkgoWriter.Printf("  could not list agent providers: %v\n", err)
		return
	}
	GinkgoWriter.Printf("  agent providers (%d):\n", len(providers))
	for _, p := range providers {
		GinkgoWriter.Printf("    - service_type=%s type=%s endpoint=%s status=%s\n",
			p.ServiceType, p.Type, p.Endpoint, p.Status)
	}
}

func requireEnvironmentAgent() {
	if !environmentAgentReady {
		Skip("Environment agent not available (set DCM_ENVIRONMENT_AGENT_URL or DCM_AGENT_URL and deploy environment-agent)")
	}
}

func doEnvironmentAgentRequest(method, path string, body string) (*http.Response, error) {
	reqURL := environmentAgentBaseURL + path

	var reqBody io.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, reqURL, reqBody)
	if err != nil {
		return nil, err
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	return httpClient.Do(req)
}

func fetchEnvironmentAgentProviders() ([]agentProvider, error) {
	var all []agentProvider
	token := ""

	for {
		path := "/providers"
		if token != "" {
			path += "?page_token=" + url.QueryEscape(token)
		}

		resp, err := doEnvironmentAgentRequest(http.MethodGet, path, "")
		if err != nil {
			return nil, err
		}

		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GET %s returned %d", path, resp.StatusCode)
		}

		var page agentProviderList
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("decode providers list: %w", err)
		}
		all = append(all, page.Results...)
		if page.NextPageToken == "" {
			break
		}
		token = page.NextPageToken
	}

	return all, nil
}

func listEnvironmentAgentProviders() []agentProvider {
	all, err := fetchEnvironmentAgentProviders()
	Expect(err).NotTo(HaveOccurred())
	return all
}

func storageProvidersFromAgent() []agentProvider {
	var matched []agentProvider
	for _, p := range listEnvironmentAgentProviders() {
		if p.ServiceType == "storage" {
			matched = append(matched, p)
		}
	}
	return matched
}
