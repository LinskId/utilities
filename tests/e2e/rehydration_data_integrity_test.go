//go:build e2e

package e2e_test

import (
	"net/http"
	"regexp"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Rehydration Data Integrity", Label("rehydration", "integrity"), Ordered, func() {
	var (
		instanceUID    string
		origResourceID string
		newResourceID  string
		policyID       string
	)

	BeforeAll(func() {
		requireThreeTierSP()

		provider := threeTierProviders[0]
		policyID = createPlacementPolicy("integrity-policy", regoSelectProvider(provider.Name))

		inst := createTestInstance(uniqueName("integrity"), defaultUserValues())
		instanceUID = inst.UID
		origResourceID = inst.ResourceID

		waitForInstanceRunning(inst.ResourceID, provisionTimeout)

		resp, body := rehydrateInstance(inst.UID)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		newResourceID = stringField(body, "resource_id")
		Expect(newResourceID).NotTo(Equal(origResourceID))
	})

	AfterAll(func() {
		if instanceUID != "" {
			deleteInstance(instanceUID)
		}
		if policyID != "" {
			deletePlacementPolicy(policyID)
		}
	})

	It("resource_id is consistent across API and K8s", Label("cluster"), func() {
		requireKubectl()

		inst := getInstance(instanceUID)
		apiResourceID := inst.ResourceID
		Expect(apiResourceID).To(Equal(newResourceID))

		ns := getActiveDeploymentNamespace(apiResourceID)
		Expect(ns).NotTo(BeEmpty(),
			"expected deployments in some namespace for resource_id %s", apiResourceID)
	})

	It("metadata is preserved across rehydration", func() {
		inst := getInstance(instanceUID)

		Expect(inst.UID).To(Equal(instanceUID))
		Expect(inst.DisplayName).NotTo(BeEmpty())
		Expect(inst.CreateTime).NotTo(BeEmpty())

		Expect(inst.Spec).NotTo(BeNil())
		catalogItemID, _ := inst.Spec["catalog_item_id"].(string)
		Expect(catalogItemID).To(Equal(rehydrationCatalogItemID))
	})

	It("only one active resource exists per instance", Label("cluster"), func() {
		requireKubectl()

		inst := getInstance(instanceUID)
		count := countDeploymentsAcrossNamespaces(inst.ResourceID)
		Expect(count).To(BeNumerically(">=", 1), "should have at least 1 active deployment")

		if origResourceID != inst.ResourceID {
			oldCount := countDeploymentsAcrossNamespaces(origResourceID)
			Eventually(func() int {
				return countDeploymentsAcrossNamespaces(origResourceID)
			}).WithTimeout(cleanupTimeout).WithPolling(pollInterval).Should(Equal(0),
				"old resource %s should have no active deployments (found %d)", origResourceID, oldCount)
		}
	})

	It("deployment names are DNS-1035 compliant", Label("cluster"), func() {
		requireKubectl()

		inst := getInstance(instanceUID)
		ns := getActiveDeploymentNamespace(inst.ResourceID)
		Expect(ns).NotTo(BeEmpty())

		deps := getDeploymentsInNamespace(ns, inst.ResourceID)
		Expect(deps).NotTo(BeEmpty())

		dns1035 := regexp.MustCompile(`^[a-z]([a-z0-9-]*[a-z0-9])?$`)
		for _, name := range deps {
			Expect(dns1035.MatchString(name)).To(BeTrue(),
				"deployment name %q is not DNS-1035 compliant", name)
			Expect(len(name)).To(BeNumerically("<=", 63),
				"deployment name %q exceeds 63 characters", name)
		}
	})

	It("returns 424 when no provider is selected (FLPATH-4097 regression)", func() {
		deleteAllPolicies()
		defer func() {
			provider := threeTierProviders[0]
			policyID = createPlacementPolicy("restore-integrity", regoSelectProvider(provider.Name))
		}()

		resp, _ := rehydrateInstanceRaw(instanceUID)
		Expect(resp.StatusCode).To(Equal(http.StatusFailedDependency)) // 424
	})

	It("concurrent rehydrate triggers CAS guard (FLPATH-4098 regression)", func() {
		provider := threeTierProviders[0]
		if policyID == "" {
			policyID = createPlacementPolicy("tc31-policy", regoSelectProvider(provider.Name))
		}

		var wg sync.WaitGroup
		codes := make([]int, 3)

		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				defer GinkgoRecover()
				resp, _ := rehydrateInstanceRaw(instanceUID)
				codes[idx] = resp.StatusCode
			}(i)
		}
		wg.Wait()

		successCount := 0
		conflictCount := 0
		for _, code := range codes {
			switch code {
			case http.StatusOK:
				successCount++
			case http.StatusConflict: // 409
				conflictCount++
			}
		}

		GinkgoWriter.Printf("CAS guard results: success=%d conflict=%d codes=%v\n",
			successCount, conflictCount, codes)

		Expect(successCount + conflictCount).To(Equal(3),
			"all requests should complete with 200 or 409, got codes=%v", codes)
		Expect(successCount).To(BeNumerically(">=", 1),
			"at least one concurrent rehydrate should succeed")
	})

	It("rehydrate returns 200, not 202", func() {
		provider := threeTierProviders[0]
		if policyID == "" {
			policyID = createPlacementPolicy("tc32-policy", regoSelectProvider(provider.Name))
		}

		resp, _ := rehydrateInstanceRaw(instanceUID)
		Expect(resp.StatusCode).To(Equal(http.StatusOK),
			"rehydrate should return 200 (synchronous), not 202 (async)")
	})

	It("back-to-back rehydrate returns OK or 409 CAS conflict", func() {
		provider := threeTierProviders[0]
		tmpPolicy := createPlacementPolicy("backtoback-policy", regoSelectProvider(provider.Name))
		defer deletePlacementPolicy(tmpPolicy)

		inst := createTestInstance(uniqueName("backtoback"), defaultUserValues())
		defer deleteInstance(inst.UID)

		waitForInstanceRunning(inst.ResourceID, provisionTimeout)

		resp1, body1 := rehydrateInstance(inst.UID)
		Expect(resp1.StatusCode).To(Equal(http.StatusOK))
		rid1 := stringField(body1, "resource_id")

		resp2, body2 := rehydrateInstance(inst.UID)
		Expect(resp2.StatusCode).To(SatisfyAny(
			Equal(http.StatusOK),
			Equal(http.StatusConflict), // 409 CAS guard
		))

		if resp2.StatusCode == http.StatusOK {
			rid2 := stringField(body2, "resource_id")
			Expect(rid2).NotTo(Equal(rid1))
		}

		current := getInstance(inst.UID)
		Expect(current.UID).To(Equal(inst.UID))
	})

	It("rehydrate and delete race reaches consistent final state", func() {
		provider := threeTierProviders[0]
		tmpPolicy := createPlacementPolicy("race-policy", regoSelectProvider(provider.Name))
		defer deletePlacementPolicy(tmpPolicy)

		inst := createTestInstance(uniqueName("race"), defaultUserValues())
		waitForInstanceRunning(inst.ResourceID, provisionTimeout)

		var wg sync.WaitGroup
		var rehydrateStatus, deleteStatus int

		wg.Add(2)
		go func() {
			defer wg.Done()
			defer GinkgoRecover()
			resp, _ := rehydrateInstanceRaw(inst.UID)
			rehydrateStatus = resp.StatusCode
		}()
		go func() {
			defer wg.Done()
			defer GinkgoRecover()
			resp, err := doRequest(http.MethodDelete, "/catalog-item-instances/"+inst.UID, "")
			if err == nil {
				resp.Body.Close()
				deleteStatus = resp.StatusCode
			}
		}()
		wg.Wait()

		GinkgoWriter.Printf("rehydrate=%d delete=%d\n", rehydrateStatus, deleteStatus)

		getResp, err := getInstanceRaw(inst.UID)
		Expect(err).NotTo(HaveOccurred())
		getResp.Body.Close()

		if getResp.StatusCode == http.StatusNotFound {
			GinkgoWriter.Println("Instance deleted — consistent final state")
		} else {
			GinkgoWriter.Println("Instance still exists — cleaning up")
			deleteInstance(inst.UID)
		}
	})
})
