//go:build e2e

package e2e_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	defaultStorageSPURL   = "http://localhost:8089/api/v1alpha1"
	natsStorageSubject    = "dcm.storage"
	defaultTestCapacity   = "1Gi"
	defaultTestStorageSC  = "standard"
)

var (
	storageSPBaseURL  string
	storageSPReady    bool
	storageSPNamespace string
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
