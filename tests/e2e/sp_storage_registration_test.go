//go:build e2e

package e2e_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Registration contract for external SPs: mirrors osac-service-provider's
// registration_test.go pattern (osac-service-provider#38, #43) but targets
// the environment-agent provider API instead of control-plane's removed
// /providers endpoint (control-plane#51).
//
// Label("tier-b-only") (osac-service-provider#43, DD-212): these specs never
// call the storage SP HTTP API — they only assert the registrar landed in
// environment-agent. Run them when DCM_ENVIRONMENT_AGENT_URL is set and the
// storage SP points DCM_REGISTRATION_URL at the agent (post osac#38 migration).
var _ = Describe("Storage SP registration with environment-agent", Label("sp", "storage", "tier-b-only"), func() {
	BeforeEach(func() {
		requireEnvironmentAgent()
	})

	// TC-E2E-020 analogue for storage
	It("registers exactly one storage-type provider pointing at the SP volumes endpoint", func() {
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
	})

	// TC-E2E-040 analogue — idempotent re-registration must not create duplicates
	It("does not duplicate the storage registration across a re-registration cycle", func() {
		Consistently(func() int {
			return len(storageProvidersFromAgent())
		}, 90*time.Second, 10*time.Second).Should(Equal(1),
			"storage SP periodic re-registration must stay idempotent on name, not create duplicates")
	})
})
