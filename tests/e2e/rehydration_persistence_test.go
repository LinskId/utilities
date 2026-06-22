//go:build e2e

package e2e_test

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Rehydration Persistence", Label("rehydration", "disruptive"), func() {
	BeforeEach(func() {
		requireMultiProvider()
		requirePodman()
	})

	It("deferred cleanup queue survives SPRM restart", Label("cluster"), func() {
		requireKubectl()

		providerA := threeTierProviders[0]
		providerB := threeTierProviders[1]

		policyID := createPlacementPolicy("tc42-initial",
			regoSelectProvider(providerA.Name))
		defer deletePlacementPolicy(policyID)

		inst := createTestInstance(uniqueName("tc42"), defaultUserValues())
		defer deleteInstance(inst.UID)

		waitForInstanceRunning(inst.ResourceID, provisionTimeout)
		oldResourceID := inst.ResourceID

		stopProvider(providerA)
		defer startProvider(providerA)
		waitForProviderHealth(providerA.Name, "unavailable", healthTimeout)

		deletePlacementPolicy(policyID)
		policyID = createPlacementPolicy("tc42-failover",
			regoSelectProvider(providerB.Name))

		resp, body := rehydrateInstance(inst.UID)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		newResourceID := stringField(body, "resource_id")
		Expect(newResourceID).NotTo(Equal(oldResourceID))
		waitForInstanceRunning(newResourceID, rehydrateTimeout)

		restartSPRM()

		Eventually(func() error {
			resp, err := doRequest(http.MethodGet, "/health", "")
			if err != nil {
				return err
			}
			resp.Body.Close()
			return nil
		}).WithTimeout(healthTimeout).WithPolling(pollInterval).Should(Succeed(),
			"SPRM should recover after restart")

		startProvider(providerA)
		waitForProviderHealth(providerA.Name, "ready", healthTimeout)

		if providerA.Namespace != "" {
			waitForDeploymentsGone(providerA.Namespace, oldResourceID, cleanupTimeout)
		}
	})

})
