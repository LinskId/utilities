//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	defaultStorageSPURL              = "http://localhost:8089/api/v1alpha1"
	defaultStorageRegisteredEndpoint = "embedded://storage"
	natsStorageSubject               = "dcm.storage"
	defaultTestCapacity              = "1Gi"
	// defaultCatalogStorageClass simulates catalog provider_hints.kubernetes.storage_class in SP-direct E2E.
	// "standard" matches common kind/minikube default StorageClass names; override with E2E_CATALOG_STORAGE_CLASS.
	defaultCatalogStorageClass = "standard"
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

	storageSPRegisteredEndpoint = os.Getenv("K8S_STORAGE_SP_REGISTERED_ENDPOINT")
	if storageSPRegisteredEndpoint == "" {
		storageSPRegisteredEndpoint = defaultStorageRegisteredEndpoint
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
	GinkgoWriter.Printf(
		"Storage SP ready at %s (namespace: %s, catalog hint storage_class: %q, SP default: %q)\n",
		storageSPBaseURL, storageSPNamespace, catalogStorageClassHint(), spDefaultStorageClass(),
	)
}

func initEnvironmentAgent() {
	environmentAgentBaseURL = os.Getenv("DCM_ENVIRONMENT_AGENT_URL")
	if environmentAgentBaseURL == "" {
		GinkgoWriter.Println("DCM_ENVIRONMENT_AGENT_URL unset — storage registration tests will be skipped")
		return
	}
	environmentAgentBaseURL = strings.TrimRight(environmentAgentBaseURL, "/")

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
		GinkgoWriter.Printf("    - service_type=%v type=%v endpoint=%v status=%v\n",
			p["service_type"], p["type"], p["endpoint"], p["status"])
	}
}

func requireEnvironmentAgent() {
	if !environmentAgentReady {
		Skip("Environment agent not available (set DCM_ENVIRONMENT_AGENT_URL and deploy environment-agent)")
	}
}

func requireStorageSP() {
	if !storageSPReady {
		Skip("Storage SP not available (deploy with --k8s-storage-service-provider and publish port 8089)")
	}
}

// requireClusterStoragePrereqs validates catalog hint and SP default classes before cluster tests.
func requireClusterStoragePrereqs() {
	requireCatalogStorageClass()
	requireSPDefaultStorageClass()
}

func requireCatalogStorageClass() {
	requireKubectl()
	sc := catalogStorageClassHint()
	out, err := runStorageKubectl("get", "sc", sc)
	if err != nil {
		Fail(fmt.Sprintf(
			"StorageClass %q not found on cluster (catalog provider_hints.kubernetes.storage_class). "+
				"Set E2E_CATALOG_STORAGE_CLASS to an existing class or deploy with --k8s-storage-service-provider "+
				"(writes .dcm-e2e.env). kubectl: %s",
			sc, strings.TrimSpace(out),
		))
	}
}

func requireSPDefaultStorageClass() {
	requireKubectl()
	sc := spDefaultStorageClass()
	if sc == "" {
		Fail("K8S_STORAGE_SP_DEFAULT_STORAGE_CLASS unset — deploy with --k8s-storage-service-provider " +
			"(ensure_storage_class_for_e2e sets SP_K8S_DEFAULT_STORAGE_CLASS and writes .dcm-e2e.env)")
	}
	out, err := runStorageKubectl("get", "sc", sc)
	if err != nil {
		Fail(fmt.Sprintf(
			"StorageClass %q not found on cluster (SP_K8S_DEFAULT_STORAGE_CLASS / K8S_STORAGE_SP_DEFAULT_STORAGE_CLASS). "+
				"Deploy-dcm.sh validates and writes .dcm-e2e.env. kubectl: %s",
			sc, strings.TrimSpace(out),
		))
	}
}

// catalogStorageClassHint returns the storage_class simulated as a CatalogItem default in SP-direct E2E.
// Override with E2E_CATALOG_STORAGE_CLASS when the cluster does not have a "standard" StorageClass.
func catalogStorageClassHint() string {
	if sc := os.Getenv("E2E_CATALOG_STORAGE_CLASS"); sc != "" {
		return sc
	}
	return defaultCatalogStorageClass
}

// spDefaultStorageClass returns the SP fallback when provider_hints omit storage_class (TC-2.1.6).
// Set by deploy-dcm.sh → SP_K8S_DEFAULT_STORAGE_CLASS, not by catalog.
func spDefaultStorageClass() string {
	if sc := os.Getenv("K8S_STORAGE_SP_DEFAULT_STORAGE_CLASS"); sc != "" {
		return sc
	}
	return ""
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
	deletePVCConsumer(id)
	resp, err := doStorageSPRequest(http.MethodDelete, "/volumes/"+id, "")
	if err != nil {
		GinkgoWriter.Printf("Warning: cleanup DELETE failed for volume %s: %v\n", id, err)
		return
	}
	resp.Body.Close()
}

// volumeSpec returns a JSON body with catalog-derived provider_hints (simulates SpecBuilder output).
func volumeSpec(name, capacity string) string {
	return volumeSpecWith(name, capacity, catalogStorageClassHint())
}

// volumeSpecMinimal returns a JSON body without provider_hints (SP/cluster defaults).
func volumeSpecMinimal(name, capacity string) string {
	spec := map[string]interface{}{
		"service_type": "storage",
		"capacity":     capacity,
		"metadata": map[string]interface{}{
			"name": name,
		},
	}
	body := map[string]interface{}{"spec": spec}
	data, _ := json.Marshal(body)
	return string(data)
}

// volumeSpecWith returns a JSON body with an explicit StorageClass hint.
func volumeSpecWith(name, capacity, storageClass string) string {
	return volumeSpecWithHints(name, capacity, storageClass, "", "")
}

// volumeSpecWithHints returns a JSON body with optional kubernetes provider hints.
func volumeSpecWithHints(name, capacity, storageClass, accessMode, volumeMode string) string {
	spec := map[string]interface{}{
		"service_type": "storage",
		"capacity":     capacity,
		"metadata": map[string]interface{}{
			"name": name,
		},
	}
	k8sHints := map[string]interface{}{}
	if storageClass != "" {
		k8sHints["storage_class"] = storageClass
	}
	if accessMode != "" {
		k8sHints["access_mode"] = accessMode
	}
	if volumeMode != "" {
		k8sHints["volume_mode"] = volumeMode
	}
	if len(k8sHints) > 0 {
		spec["provider_hints"] = map[string]interface{}{
			"kubernetes": k8sHints,
		}
	}
	body := map[string]interface{}{"spec": spec}
	data, _ := json.Marshal(body)
	return string(data)
}

// getStoragePVCJSON fetches a PVC by name via kubectl and returns parsed JSON.
func getStoragePVCJSON(name string) map[string]interface{} {
	out, err := runStorageKubectl("get", "pvc", name, "-o", "json")
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "failed to get PVC %s", name)

	var pvc map[string]interface{}
	ExpectWithOffset(1, json.Unmarshal([]byte(out), &pvc)).To(Succeed())
	return pvc
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

// pvcConsumerPodName returns a deterministic pod name for binding a PVC on WaitForFirstConsumer clusters.
func pvcConsumerPodName(pvcName string) string {
	const prefix = "e2e-pvc-bind-"
	name := prefix + pvcName
	if len(name) <= 63 {
		return name
	}
	return prefix + pvcName[len(pvcName)-(63-len(prefix)):]
}

// ensurePVCConsumer schedules a pod that mounts the PVC so local-path (WaitForFirstConsumer) can bind it.
func ensurePVCConsumer(pvcName string) {
	requireKubectl()

	podName := pvcConsumerPodName(pvcName)
	manifest := fmt.Sprintf(`apiVersion: v1
kind: Pod
metadata:
  name: %s
  labels:
    dcm.project/managed-by: dcm-e2e
spec:
  restartPolicy: Never
  containers:
  - name: pause
    image: registry.k8s.io/pause:3.9
    volumeMounts:
    - name: vol
      mountPath: /data
  volumes:
  - name: vol
    persistentVolumeClaim:
      claimName: %s
`, podName, pvcName)

	Expect(applyStorageManifest(manifest)).To(Succeed(), "failed to schedule PVC consumer pod for %s", pvcName)

	Eventually(func() string {
		out, err := runStorageKubectl("get", "pod", podName, "-o", "jsonpath={.status.phase}")
		if err != nil {
			return ""
		}
		return strings.TrimSpace(out)
	}).WithTimeout(60*time.Second).WithPolling(2*time.Second).Should(
		SatisfyAny(Equal("Running"), Equal("Succeeded")),
		"PVC consumer pod %s should start", podName)
}

func deletePVCConsumer(pvcName string) {
	if kubectlBin == "" {
		return
	}
	podName := pvcConsumerPodName(pvcName)
	_, _ = runStorageKubectl("delete", "pod", podName, "--ignore-not-found")
}

// describeStorageVolumeDebug returns PVC status context for RUNNING wait failures.
func describeStorageVolumeDebug(volumeID string) string {
	phase, err := runStorageKubectl("get", "pvc", volumeID,
		"-o", "jsonpath={.status.phase}/{.spec.storageClassName}")
	if err != nil {
		return fmt.Sprintf("PVC lookup failed (catalog hint storage_class: %q): %v", catalogStorageClassHint(), err)
	}
	phase = strings.TrimSpace(phase)
	events, _ := runStorageKubectl("get", "events",
		"--field-selector", fmt.Sprintf("involvedObject.name=%s", volumeID),
		"-o", "jsonpath={range .items[*]}{.reason}: {.message}{\"\\n\"}{end}")
	events = strings.TrimSpace(events)
	msg := fmt.Sprintf("PVC phase/class=%s (catalog hint storage_class=%q, SP default K8S_STORAGE_SP_DEFAULT_STORAGE_CLASS=%q)",
		phase, catalogStorageClassHint(), os.Getenv("K8S_STORAGE_SP_DEFAULT_STORAGE_CLASS"))
	if events != "" {
		msg += "; events: " + events
	}
	return msg
}

// waitForStorageVolumeRunning waits until the SP reports RUNNING, scheduling a PVC consumer when needed.
func waitForStorageVolumeRunning(volumeID string, timeout time.Duration) {
	requireCatalogStorageClass()
	ensurePVCConsumer(volumeID)

	Eventually(func() string {
		resp, err := doStorageSPRequest(http.MethodGet, "/volumes/"+volumeID, "")
		if err != nil {
			return ""
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return ""
		}
		var body map[string]interface{}
		decodeJSON(resp, &body)
		s, _ := body["status"].(string)
		return s
	}).WithTimeout(timeout).WithPolling(3*time.Second).Should(Equal("RUNNING"),
		"volume %s did not reach RUNNING; %s", volumeID, describeStorageVolumeDebug(volumeID))
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

// fetchEnvironmentAgentProviders returns all providers from the environment agent.
func fetchEnvironmentAgentProviders() ([]map[string]interface{}, error) {
	var all []map[string]interface{}
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
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("GET %s returned %d", path, resp.StatusCode)
		}

		var body map[string]interface{}
		decodeJSON(resp, &body)

		results, ok := body["results"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("providers list response missing results array")
		}

		for _, p := range results {
			provider, ok := p.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("invalid provider entry in results")
			}
			all = append(all, provider)
		}

		next, _ := body["next_page_token"].(string)
		if next == "" {
			break
		}
		token = next
	}

	return all, nil
}

// listEnvironmentAgentProviders returns all providers from the environment agent,
// following pagination tokens until exhausted.
func listEnvironmentAgentProviders() []map[string]interface{} {
	all, err := fetchEnvironmentAgentProviders()
	Expect(err).NotTo(HaveOccurred())
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
