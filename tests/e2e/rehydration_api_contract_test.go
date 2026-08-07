//go:build e2e

package e2e_test

import (
	"encoding/json"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Rehydration API Contract", Label("rehydration", "contract"), func() {
	BeforeEach(func() {
		requireThreeTierSP()
	})

	It("200 response contains required fields", func() {
		provider := threeTierProviders[0]
		policyID := createPlacementPolicy("tc36-policy", regoSelectProvider(provider.Name))
		defer deletePlacementPolicy(policyID)

		inst := createTestInstance(uniqueName("tc36"), defaultUserValues())
		defer deleteInstance(inst.UID)

		waitForInstanceRunning(inst.ResourceID, provisionTimeout)

		resp, body := rehydrateInstance(inst.UID)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		Expect(body).To(HaveKey("uid"))
		Expect(body).To(HaveKey("spec"))
		Expect(body).To(HaveKey("display_name"))
		Expect(body).To(HaveKey("api_version"))
		Expect(body).To(HaveKey("create_time"))
		Expect(body).To(HaveKey("update_time"))
		Expect(body).To(HaveKey("run_id"), "rehydrate must return run_id after control-plane#39")

		Expect(stringField(body, "uid")).NotTo(BeEmpty())
		Expect(stringField(body, "api_version")).To(Equal("v1alpha1"))
		Expect(stringField(body, "run_id")).NotTo(BeEmpty())

		spec, ok := body["spec"].(map[string]interface{})
		Expect(ok).To(BeTrue(), "spec should be a map")
		Expect(spec).To(HaveKey("catalog_item_id"))
		Expect(firstResourceID(body)).NotTo(BeEmpty(),
			"rehydrate must yield a discoverable placement resource ID")
	})

	It("404 response conforms to RFC 7807", func() {
		resp, rawBody := rehydrateInstanceRaw("nonexistent-" + uniqueName("tc37"))
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))

		var body map[string]interface{}
		Expect(json.Unmarshal(rawBody, &body)).To(Succeed())

		Expect(body).To(HaveKey("type"))
		Expect(body).To(HaveKey("title"))
		Expect(body).To(HaveKey("status"))
		Expect(body).To(HaveKey("detail"))

		status, ok := body["status"].(float64)
		if ok {
			Expect(int(status)).To(Equal(404))
		}
	})

	It("424 response conforms to RFC 7807", func() {
		provider := threeTierProviders[0]
		policyID := createPlacementPolicy("tc38-policy", regoSelectProvider(provider.Name))

		inst := createTestInstance(uniqueName("tc38"), defaultUserValues())
		defer deleteInstance(inst.UID)

		waitForInstanceRunning(inst.ResourceID, provisionTimeout)

		deletePlacementPolicy(policyID)
		deleteAllPolicies()

		resp, rawBody := rehydrateInstanceRaw(inst.UID)
		Expect(resp.StatusCode).To(Equal(http.StatusFailedDependency)) // 424

		var body map[string]interface{}
		Expect(json.Unmarshal(rawBody, &body)).To(Succeed())

		Expect(body).To(HaveKey("type"))
		Expect(body).To(HaveKey("title"))
		Expect(body).To(HaveKey("status"))
		Expect(body).To(HaveKey("detail"))
	})

	PIt("422 response conforms to RFC 7807", func() {
		// Hard to induce at E2E — requires specific provider error conditions
	})
})
