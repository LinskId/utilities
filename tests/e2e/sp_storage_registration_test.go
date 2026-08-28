//go:build e2e

package e2e_test

import (
	"net/http"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Registration contract for embedded storage SP in environment-agent
// (AGENT_EMBEDDED_SPS=storage → endpoint embedded://storage at agent startup).
//
// Label("registration") — requires deploy-dcm --environment-agent and
// DCM_ENVIRONMENT_AGENT_URL (written to .dcm-e2e.env).
var _ = Describe("Embedded storage SP registration with environment-agent", Label("sp", "storage", "registration"), func() {
	BeforeEach(func() {
		requireEnvironmentAgent()
	})

	// TC-E2E-020 analogue for embedded storage
	It("registers exactly one embedded storage provider", func() {
		var found map[string]interface{}
		Eventually(func() int {
			providers := storageProvidersFromAgent()
			found = nil
			for _, p := range providers {
				found = p
			}
			return len(providers)
		}, 60*time.Second, 2*time.Second).Should(Equal(1),
			"expected exactly one storage provider registered with environment-agent")

		Expect(found).NotTo(BeNil())
		Expect(found["service_type"]).To(Equal("storage"))
		Expect(found["endpoint"]).To(Equal(storageSPRegisteredEndpoint))
		Expect(found["type"]).To(Equal("embedded"))
	})

	// TC-E2E-040 analogue — embedded registration must stay idempotent across agent restarts
	It("keeps a single embedded storage provider after agent restart", Label("disruptive"), func() {
		requirePodman()

		providers := storageProvidersFromAgent()
		Expect(providers).To(HaveLen(1), "expected exactly one storage provider before restart")
		providerID, ok := providers[0]["id"].(string)
		Expect(ok).To(BeTrue(), "storage provider should have an id")
		Expect(providers[0]["endpoint"]).To(Equal(storageSPRegisteredEndpoint))

		agentContainer := findComposeContainer("environment-agent")

		By("stopping and starting the environment-agent container")
		out, err := exec.Command(podmanBin, "stop", "-t", "5", agentContainer).CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "failed to stop environment-agent: %s", string(out))
		out, err = exec.Command(podmanBin, "start", agentContainer).CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "failed to start environment-agent: %s", string(out))

		By("waiting for environment-agent to become healthy again")
		Eventually(func() int {
			resp, err := httpClient.Get(environmentAgentBaseURL + "/health")
			if err != nil {
				return 0
			}
			defer resp.Body.Close()
			return resp.StatusCode
		}, 60*time.Second, 2*time.Second).Should(Equal(http.StatusOK),
			"environment-agent should be healthy after restart")

		By("verifying embedded storage registration is not duplicated")
		var afterRestart map[string]interface{}
		Eventually(func() int {
			matched := storageProvidersFromAgent()
			afterRestart = nil
			for _, p := range matched {
				afterRestart = p
			}
			return len(matched)
		}, 60*time.Second, 2*time.Second).Should(Equal(1),
			"embedded storage registration must stay idempotent after agent restart")

		Expect(afterRestart).NotTo(BeNil())
		Expect(afterRestart["id"]).To(Equal(providerID), "re-registration should keep the same provider id")
		Expect(afterRestart["endpoint"]).To(Equal(storageSPRegisteredEndpoint))
		Expect(afterRestart["type"]).To(Equal("embedded"))

		By("verifying the environment-agent container is running after restart")
		Eventually(func() string {
			state, _ := exec.Command(podmanBin, "inspect", "--format", "{{.State.Status}}", agentContainer).CombinedOutput()
			return strings.TrimSpace(string(state))
		}, 15*time.Second, 1*time.Second).Should(Equal("running"),
			"environment-agent container should be in running state after restart")
	})
})
