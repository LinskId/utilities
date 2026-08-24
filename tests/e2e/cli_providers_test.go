//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Skipped: the DCM CLI has not been updated for the /providers → /agents migration.
// Re-enable once the CLI supports `dcm agent` commands (see FLPATH-XXXX).
var _ = PDescribe("CLI: sp provider commands", Label("cli"), func() {
	Context("read operations", Ordered, func() {
		var providerID string
		providerName := fmt.Sprintf("e2e-cli-provider-%d", time.Now().UnixNano())

		// Create a provider via API so the CLI has something to read.
		BeforeAll(func() {
			payload := fmt.Sprintf(`{
				"name": %q,
				"endpoint": "https://example.com/api",
				"service_type": "vm",
				"schema_version": "v1alpha1"
			}`, providerName)

			resp, err := doRequest(http.MethodPost, "/providers", payload)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))

			var body map[string]interface{}
			decodeJSON(resp, &body)

			id, ok := body["id"].(string)
			Expect(ok).To(BeTrue(), "id should be a string")
			providerID = id
		})

		AfterAll(func() {
			if providerID != "" {
				resp, err := doRequest(http.MethodDelete, "/providers/"+providerID, "")
				if err != nil {
					GinkgoWriter.Printf("Warning: cleanup failed for provider %s: %v\n", providerID, err)
				}
				if resp != nil {
					resp.Body.Close()
				}
			}
		})

		It("lists providers", func() {
			stdout, stderr, exitCode := runDCM("sp", "provider", "list")

			Expect(exitCode).To(Equal(0), "stderr: %s", stderr)
			Expect(stdout).To(ContainSubstring(providerID))
		})

		It("lists providers in JSON format", func() {
			stdout, stderr, exitCode := runDCM("sp", "provider", "list", "--output", "json")

			Expect(exitCode).To(Equal(0), "stderr: %s", stderr)

			var result map[string]interface{}
			Expect(json.Unmarshal([]byte(stdout), &result)).To(Succeed())
			Expect(result).To(HaveKey("results"))
		})

		It("gets a provider by ID", func() {
			Expect(providerID).NotTo(BeEmpty(), "provider must be created first")

			stdout, stderr, exitCode := runDCM("sp", "provider", "get", providerID)

			Expect(exitCode).To(Equal(0), "stderr: %s", stderr)
			Expect(stdout).To(ContainSubstring(providerName))
		})
	})

	Context("error handling", func() {
		It("returns exit code 1 for non-existent provider", func() {
			_, stderr, exitCode := runDCM("sp", "provider", "get", "does-not-exist")

			Expect(exitCode).To(Equal(1))
			Expect(stderr).NotTo(BeEmpty())
		})
	})
})
