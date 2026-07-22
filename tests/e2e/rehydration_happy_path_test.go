//go:build e2e

package e2e_test

import (
	"fmt"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Rehydration Happy Path", Label("rehydration", "happy-path"), Ordered, func() {
	var (
		instanceUID    string
		origResourceID string
		policyID       string
	)

	BeforeAll(func() {
		requireThreeTierSP()

		provider := threeTierProviders[0]
		policyID = createPlacementPolicy("rehydrate-happy-path",
			regoSelectProvider(provider.Name))
	})

	AfterAll(func() {
		if instanceUID != "" {
			deleteInstance(instanceUID)
		}
		if policyID != "" {
			deletePlacementPolicy(policyID)
		}
	})

	It("creates a pet-clinic instance and reaches RUNNING", func() {
		inst := createTestInstance(
			uniqueName("rehydrate-happy"),
			defaultUserValues(),
		)

		Expect(inst.UID).NotTo(BeEmpty())
		Expect(inst.ResourceID).NotTo(BeEmpty())
		instanceUID = inst.UID
		origResourceID = inst.ResourceID

		GinkgoWriter.Printf("Created instance UID=%s ResourceID=%s\n", inst.UID, inst.ResourceID)

		waitForInstanceRunning(inst.ResourceID, provisionTimeout)
	})

	It("rehydrates the running instance", func() {
		Expect(instanceUID).NotTo(BeEmpty(), "create test must pass first")

		resp, body := rehydrateInstance(instanceUID)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		newResourceID := firstResourceID(body)
		Expect(newResourceID).NotTo(BeEmpty())
		Expect(newResourceID).NotTo(Equal(origResourceID))

		GinkgoWriter.Printf("Rehydrated: old ResourceID=%s new ResourceID=%s\n",
			origResourceID, newResourceID)
	})

	It("preserves UID and assigns new resource_id", func() {
		Expect(instanceUID).NotTo(BeEmpty(), "create test must pass first")

		inst := getInstance(instanceUID)
		Expect(inst.UID).To(Equal(instanceUID))
		Expect(inst.ResourceID).NotTo(Equal(origResourceID))

		GinkgoWriter.Printf("Confirmed: UID=%s stable, ResourceID changed from %s to %s\n",
			inst.UID, origResourceID, inst.ResourceID)
	})

	It("new K8s resources exist in provider's namespace", Label("cluster"), func() {
		requireKubectl()
		Expect(instanceUID).NotTo(BeEmpty(), "create test must pass first")

		inst := getInstance(instanceUID)
		waitForInstanceRunning(inst.ResourceID, rehydrateTimeout)

		provider := threeTierProviders[0]
		ns := provider.Namespace
		if ns == "" {
			ns = "default"
		}

		Eventually(func() int {
			deps := getDeploymentsInNamespace(ns, inst.ResourceID)
			return len(deps)
		}).WithTimeout(rehydrateTimeout).WithPolling(pollInterval).Should(
			BeNumerically(">=", 1),
			"expected deployments with resource_id %s in namespace %s", inst.ResourceID, ns)
	})

	It("old K8s resources are cleaned up", Label("cluster"), func() {
		requireKubectl()
		Expect(origResourceID).NotTo(BeEmpty(), "create test must pass first")

		provider := threeTierProviders[0]
		ns := provider.Namespace
		if ns == "" {
			ns = "default"
		}

		waitForDeploymentsGone(ns, origResourceID, cleanupTimeout)
	})

	It("application is functional after rehydrate", Label("cluster"), func() {
		requireKubectl()
		Expect(instanceUID).NotTo(BeEmpty(), "create test must pass first")

		inst := getInstance(instanceUID)
		waitForInstanceRunning(inst.ResourceID, rehydrateTimeout)

		provider := threeTierProviders[0]
		ns := provider.Namespace
		if ns == "" {
			ns = "default"
		}

		Eventually(func() error {
			out, err := runKubectlInNamespace(ns,
				"get", "svc",
				"-l", "dcm-resource-id="+inst.ResourceID,
				"-o", "jsonpath={.items[*].metadata.name}")
			if err != nil {
				return fmt.Errorf("kubectl get svc failed: %s", out)
			}
			names := strings.Fields(strings.TrimSpace(out))
			if len(names) == 0 {
				return fmt.Errorf("no services found for resource_id %s", inst.ResourceID)
			}
			return nil
		}).WithTimeout(rehydrateTimeout).WithPolling(pollInterval).Should(Succeed(),
			"expected services for resource_id %s in namespace %s", inst.ResourceID, ns)
	})

	It("second rehydrate produces same behavior", func() {
		Expect(instanceUID).NotTo(BeEmpty(), "create test must pass first")

		preInst := getInstance(instanceUID)
		preResourceID := preInst.ResourceID

		resp, body := rehydrateInstance(instanceUID)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		newResourceID := firstResourceID(body)
		Expect(newResourceID).NotTo(BeEmpty())
		Expect(newResourceID).NotTo(Equal(preResourceID))

		postInst := getInstance(instanceUID)
		Expect(postInst.UID).To(Equal(instanceUID))
		Expect(postInst.ResourceID).To(Equal(newResourceID))

		GinkgoWriter.Printf("Second rehydrate: ResourceID %s → %s\n", preResourceID, newResourceID)
	})
})
