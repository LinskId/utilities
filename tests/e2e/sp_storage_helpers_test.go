//go:build e2e

package e2e_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	defaultStorageSPURL              = "http://localhost:8089/api/v1alpha1"
	defaultStorageRegisteredEndpoint = "http://k8s-storage-service-provider:8080/api/v1alpha1/volumes"
	natsStorageSubject               = "dcm.storage"
	defaultTestCapacity              = "1Gi"
	defaultTestStorageSC             = "standard"
)

var (
	storageSPBaseURL            string
	storageSPReady              bool
	storageSPNamespace          string
	storageSPRegisteredEndpoint string
	environmentAgentBaseURL     string
	environmentAgentReady       bool
)

func initStorageSP() {
	storageSPBaseURL = os.Getenv("DCM_STORAGE_SP_URL")
	if storageSPBaseURL == "" {
		storageSPBaseURL = defaultStorageSPURL
	}
	storageSPBaseURL = strings.TrimRight(storageSPBaseURL, "/")

	storageSPNamespace = os.Getenv("K8S_STORAGE_SP_NAMESPACE")
	if storageSPNamespace == "" {
		storageSPNamespace = "default"
	}

	resp, err := httpClient.Get(storageSPBaseURL + "/volumes/health")
	if err != nil {
		GinkgoWriter.Printf("Storage SP not reachable at %s: %v — storage SP tests will be skipped\n", storageSPBaseURL, err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		GinkgoWriter.Printf("Storage SP health returned %d — storage SP tests will be skipped\n", resp.StatusCode)
		return
	}
	storageSPReady = true
	GinkgoWriter.Printf("Storage SP ready at %s (namespace: %s)\n", storageSPBaseURL, storageSPNamespace)

	storageSPRegisteredEndpoint = os.Getenv("K8S_STORAGE_SP_REGISTERED_ENDPOINT")
	if storageSPRegisteredEndpoint == "" {
		storageSPRegisteredEndpoint = defaultStorageRegisteredEndpoint
	}
}

func initEnvironmentAgent() {
	environmentAgentBaseURL = os.Getenv("DCM_ENVIRONMENT_AGENT_URL")
	if environmentAgentBaseURL == "" {
		GinkgoWriter.Println("DCM_ENVIRONMENT_AGENT_URL unset — tier-b storage registration tests will be skipped")
		return
	}
	environmentAgentBaseURL = strings.TrimRight(environmentAgentBaseURL, "/")

	resp, err := httpClient.Get(environmentAgentBaseURL + "/health")
	if err != nil {
		GinkgoWriter.Printf("Environment agent not reachable at %s: %v — tier-b storage tests will be skipped\n", environmentAgentBaseURL, err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		GinkgoWriter.Printf("Environment agent health returned %d — tier-b storage tests will be skipped\n", resp.StatusCode)
		return
	}
	environmentAgentReady = true
	GinkgoWriter.Printf("Environment agent ready at %s\n", environmentAgentBaseURL)
}

func requireEnvironmentAgent() {
	if !environmentAgentReady {
		Skip("Environment agent not available (set DCM_ENVIRONMENT_AGENT_URL and deploy environment-agent; follows osac-service-provider#38)")
	}
}

func requireStorageSP() {
	if !storageSPReady {
		Skip("Storage SP not available (deploy with --k8s-storage-service-provider and publish port 8089)")
	}
}

func testStorageClass() string {
	if sc := os.Getenv("K8S_STORAGE_SP_DEFAULT_STORAGE_CLASS"); sc != "" {
		return sc
	}
	return defaultTestStorageSC
}

// doStorageSPRequest sends a request to the storage SP's direct API.
func doStorageSPRequest(method, path string, body string) (*http.Response, error) {
	url := storageSPBaseURL + path

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

// createTestVolume creates a volume via the SP API and returns the parsed response body.
func createTestVolume(spec string) map[string]interface{} {
	resp, err := doStorageSPRequest(http.MethodPost, "/volumes", spec)
	Expect(err).NotTo(HaveOccurred())
	Expect(resp.StatusCode).To(Equal(http.StatusCreated),
		"create volume failed with status %d", resp.StatusCode)

	var body map[string]interface{}
	decodeJSON(resp, &body)
	Expect(body).To(HaveKey("id"))
	return body
}

// deleteTestVolume removes a volume by ID, ignoring 404 (already gone).
func deleteTestVolume(id string) {
	resp, err := doStorageSPRequest(http.MethodDelete, "/volumes/"+id, "")
	if err != nil {
		GinkgoWriter.Printf("Warning: cleanup DELETE failed for volume %s: %v\n", id, err)
		return
	}
	resp.Body.Close()
}

// volumeSpec returns a JSON body for creating a test volume per the OpenAPI schema.
func volumeSpec(name, capacity string) string {
	return volumeSpecWith(name, capacity, testStorageClass())
}

// volumeSpecWith returns a JSON body with an explicit StorageClass hint.
func volumeSpecWith(name, capacity, storageClass string) string {
	spec := map[string]interface{}{
		"service_type": "storage",
		"capacity":     capacity,
		"metadata": map[string]interface{}{
			"name": name,
		},
	}
	if storageClass != "" {
		spec["provider_hints"] = map[string]interface{}{
			"kubernetes": map[string]interface{}{
				"storage_class": storageClass,
			},
		}
	}
	body := map[string]interface{}{"spec": spec}
	data, _ := json.Marshal(body)
	return string(data)
}

// runStorageKubectl executes kubectl/oc in the storage SP namespace.
func runStorageKubectl(args ...string) (string, error) {
	fullArgs := append([]string{"-n", storageSPNamespace}, args...)
	cmd := exec.Command(kubectlBin, fullArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		GinkgoWriter.Printf("kubectl %v failed: %s\n", args, string(out))
	}
	return string(out), err
}

// applyStorageManifest applies a Kubernetes manifest in the storage SP namespace.
func applyStorageManifest(manifest string) error {
	cmd := exec.Command(kubectlBin, "-n", storageSPNamespace, "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		GinkgoWriter.Printf("kubectl apply failed: %s\n", string(out))
	}
	return err
}

// doEnvironmentAgentRequest sends a request to the environment agent API.
func doEnvironmentAgentRequest(method, path string, body string) (*http.Response, error) {
	url := environmentAgentBaseURL + path

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

// listEnvironmentAgentProviders returns all providers from the environment agent,
// following pagination tokens until exhausted.
func listEnvironmentAgentProviders() []map[string]interface{} {
	var all []map[string]interface{}
	token := ""

	for {
		path := "/providers"
		if token != "" {
			path += "?page_token=" + url.QueryEscape(token)
		}

		resp, err := doEnvironmentAgentRequest(http.MethodGet, path, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		var body map[string]interface{}
		decodeJSON(resp, &body)

		providers, ok := body["providers"].([]interface{})
		Expect(ok).To(BeTrue(), "providers list response missing providers array")

		for _, p := range providers {
			provider, ok := p.(map[string]interface{})
			Expect(ok).To(BeTrue())
			all = append(all, provider)
		}

		next, _ := body["next_page_token"].(string)
		if next == "" {
			break
		}
		token = next
	}

	return all
}

func storageProvidersFromAgent() []map[string]interface{} {
	var matched []map[string]interface{}
	for _, p := range listEnvironmentAgentProviders() {
		if st, _ := p["service_type"].(string); st == "storage" {
			matched = append(matched, p)
		}
	}
	return matched
}
