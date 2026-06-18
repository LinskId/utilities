//go:build e2e

package e2e_test

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Rehydration Policy", Label("rehydration", "policy"), func() {
	BeforeEach(func() {
		requireThreeTierSP()
	})

	Context("sovereignty", Label("disruptive"), func() {
		It("placement policy constrains instance to provider group", Label("cluster"), func() {
			requireThreeProviders()
			requirePodman()

			eastProviders := providersByRegion["east"]
			Expect(len(eastProviders)).To(BeNumerically(">=", 2),
				"need at least 2 east-region providers for sovereignty tests")

			policyID := createPlacementPolicy("sovereignty-east",
				regoSelectProvider(eastProviders[0].Name))
			defer deletePlacementPolicy(policyID)

			inst := createTestInstance(uniqueName("tc11"), defaultUserValues())
			defer deleteInstance(inst.UID)

			waitForInstanceRunning(inst.ResourceID, provisionTimeout)

			current := getInstance(inst.UID)
			GinkgoWriter.Printf("instance placed, resource_id=%s\n", current.ResourceID)

			if kubectlAvailable {
				ns := getActiveDeploymentNamespace(current.ResourceID)
				Expect(ns).To(SatisfyAny(
					Equal(eastProviders[0].Namespace),
					Equal(eastProviders[1].Namespace),
				), "instance should be in an east-region namespace, got %s", ns)
			}
		})

		It("failover stays within provider group", Label("cluster"), func() {
			requireThreeProviders()
			requirePodman()

			eastProviders := providersByRegion["east"]
			Expect(len(eastProviders)).To(BeNumerically(">=", 2))

			policyID := createPlacementPolicy("sovereignty-failover",
				regoSelectProvider(eastProviders[0].Name))
			defer deletePlacementPolicy(policyID)

			inst := createTestInstance(uniqueName("tc12"), defaultUserValues())
			defer deleteInstance(inst.UID)

			waitForInstanceRunning(inst.ResourceID, provisionTimeout)

			stopProvider(eastProviders[0])
			defer startProvider(eastProviders[0])

			waitForProviderHealth(eastProviders[0].Name, "unavailable", healthTimeout)

			deletePlacementPolicy(policyID)
			policyID = createPlacementPolicy("sovereignty-failover-2",
				regoSelectProvider(eastProviders[1].Name))

			resp, body := rehydrateInstance(inst.UID)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			newResourceID := stringField(body, "resource_id")
			Expect(newResourceID).NotTo(Equal(inst.ResourceID))

			if kubectlAvailable {
				waitForInstanceRunning(newResourceID, rehydrateTimeout)
				ns := getActiveDeploymentNamespace(newResourceID)
				Expect(ns).To(Equal(eastProviders[1].Namespace),
					"failover should stay in east region, got namespace %s", ns)
			}
		})

		It("rejects rehydration when all group providers are down", func() {
			requireThreeProviders()
			requirePodman()

			eastProviders := providersByRegion["east"]
			Expect(len(eastProviders)).To(BeNumerically(">=", 2))

			policyID := createPlacementPolicy("sovereignty-reject",
				regoSelectProvider(eastProviders[0].Name))
			defer deletePlacementPolicy(policyID)

			inst := createTestInstance(uniqueName("tc13"), defaultUserValues())
			defer deleteInstance(inst.UID)

			waitForInstanceRunning(inst.ResourceID, provisionTimeout)

			for _, p := range eastProviders {
				if p.ContainerName != "" {
					stopProvider(p)
					defer startProvider(p)
				}
			}

			for _, p := range eastProviders {
				waitForProviderHealth(p.Name, "unavailable", healthTimeout)
			}

			resp, _ := rehydrateInstanceRaw(inst.UID)
			Expect(resp.StatusCode).To(SatisfyAny(
				Equal(http.StatusNotAcceptable),  // 406 policy rejected
				Equal(http.StatusFailedDependency), // 424 policy dependency
				Equal(422),                        // provider error
			), "expected 406/424/422 when all group providers are down")
		})
	})

	Context("intent preservation", func() {
		It("user_values are preserved across rehydration", func() {
			provider := threeTierProviders[0]
			policyID := createPlacementPolicy("tc24-policy", regoSelectProvider(provider.Name))
			defer deletePlacementPolicy(policyID)

			userValues := defaultUserValues()

			inst := createTestInstance(uniqueName("tc24"), userValues)
			defer deleteInstance(inst.UID)

			waitForInstanceRunning(inst.ResourceID, provisionTimeout)

			beforeSpec := getInstance(inst.UID).Spec

			resp, _ := rehydrateInstance(inst.UID)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			afterSpec := getInstance(inst.UID).Spec

			beforeUV, _ := beforeSpec["user_values"]
			afterUV, _ := afterSpec["user_values"]
			Expect(afterUV).To(Equal(beforeUV),
				"user_values should be identical before and after rehydration")
		})

		It("policy change causes rehydration to select different provider", Label("cluster"), func() {
			requireMultiProvider()

			providerA := threeTierProviders[0]
			providerB := threeTierProviders[1]

			policyID := createPlacementPolicy("tc25-initial", regoSelectProvider(providerA.Name))
			defer deletePlacementPolicy(policyID)

			inst := createTestInstance(uniqueName("tc25"), defaultUserValues())
			defer deleteInstance(inst.UID)

			waitForInstanceRunning(inst.ResourceID, provisionTimeout)

			deletePlacementPolicy(policyID)
			policyID = createPlacementPolicy("tc25-swapped", regoSelectProvider(providerB.Name))

			resp, body := rehydrateInstance(inst.UID)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			newResourceID := stringField(body, "resource_id")
			Expect(newResourceID).NotTo(Equal(inst.ResourceID))

			if kubectlAvailable && providerB.Namespace != "" {
				waitForInstanceRunning(newResourceID, rehydrateTimeout)
				ns := getActiveDeploymentNamespace(newResourceID)
				Expect(ns).To(Equal(providerB.Namespace),
					"workload should move to provider B's namespace %s, got %s",
					providerB.Namespace, ns)
			}
		})
	})
})
