//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const defaultGatewayURL = "http://localhost:8080/api/v1alpha1"

var (
	gatewayBaseURL string
	httpClient     *http.Client
)

var _ = BeforeSuite(func() {
	gatewayBaseURL = os.Getenv("DCM_GATEWAY_URL")
	if gatewayBaseURL == "" {
		gatewayBaseURL = defaultGatewayURL
	}
	gatewayBaseURL = strings.TrimRight(gatewayBaseURL, "/")

	httpClient = &http.Client{Timeout: 10 * time.Second}

	GinkgoWriter.Printf("Using gateway URL: %s\n", gatewayBaseURL)

	// Wait for the stack to be reachable before running any tests.
	Eventually(func() error {
		resp, err := httpClient.Get(gatewayBaseURL + "/health")
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("health check returned %d", resp.StatusCode)
		}
		return nil
	}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(Succeed())

	// Resolve CLI binary (tests skip gracefully if not found).
	initCLI()

	// Probe service providers (tests skip gracefully if not deployed).
	initContainerSP()
	initAcmClusterSP()

	// Resolve cluster CLI for tests that need kubectl/oc.
	initKubectl()

	// Check podman for infrastructure disruption tests.
	initPodman()

	// Create catalog item and discover SPs for rehydration tests.
	initRehydrationCatalogItem()
	initThreeTierSP()
})

// doRequest builds a full URL from a relative path, sends the request, and
// returns the response. The caller is responsible for closing the body.
func doRequest(method, path string, body string) (*http.Response, error) {
	url := gatewayBaseURL + path

	var reqBody io.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, err
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	return httpClient.Do(req)
}

// readBody reads and closes the response body, returning the raw bytes.
func readBody(resp *http.Response) []byte {
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	Expect(err).NotTo(HaveOccurred())
	return data
}

// decodeJSON reads the response body and unmarshals it into the target.
func decodeJSON(resp *http.Response, target interface{}) {
	data := readBody(resp)
	Expect(json.Unmarshal(data, target)).To(Succeed())
}
