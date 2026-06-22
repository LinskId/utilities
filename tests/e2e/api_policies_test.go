//go:build e2e

package e2e_test

import (
	"fmt"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Policies API", func() {
	Context("CRUD lifecycle", Ordered, func() {
		var policyID string
		policyDisplayName := fmt.Sprintf("e2e-test-policy-%d", time.Now().UnixNano())

		AfterAll(func() {
			if policyID != "" {
				resp, err := doRequest(http.MethodDelete, "/policies/"+policyID, "")
				if err != nil {
					GinkgoWriter.Printf("Warning: cleanup failed for policy %s: %v\n", policyID, err)
				}
				if resp != nil {
					resp.Body.Close()
				}
			}
		})

		It("creates a policy", func() {
			payload := fmt.Sprintf(`{
				"display_name": %q,
				"policy_type": "GLOBAL",
				"priority": 100,
				"description": "Created by E2E test suite",
				"rego_code": "package authz\n\nmain := {\"rejected\": false}"
			}`, policyDisplayName)

			resp, err := doRequest(http.MethodPost, "/policies", payload)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))

			var body map[string]interface{}
			decodeJSON(resp, &body)
			Expect(body).To(HaveKey("id"))
			Expect(body["display_name"]).To(Equal(policyDisplayName))

			id, ok := body["id"].(string)
			Expect(ok).To(BeTrue(), "id should be a string")
			policyID = id
		})

		It("gets the policy by ID", func() {
			Expect(policyID).NotTo(BeEmpty(), "policy must be created first")

			resp, err := doRequest(http.MethodGet, "/policies/"+policyID, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body map[string]interface{}
			decodeJSON(resp, &body)
			Expect(body["display_name"]).To(Equal(policyDisplayName))
		})

		It("lists policies and includes the created one", func() {
			Expect(policyID).NotTo(BeEmpty(), "policy must be created first")

			resp, err := doRequest(http.MethodGet, "/policies", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body map[string]interface{}
			decodeJSON(resp, &body)
			Expect(body).To(HaveKey("policies"))

			policies, ok := body["policies"].([]interface{})
			Expect(ok).To(BeTrue())

			var found bool
			for _, p := range policies {
				policy, ok := p.(map[string]interface{})
				Expect(ok).To(BeTrue(), "policy entry should be a map")
				if policy["id"] == policyID {
					found = true
					break
				}
			}
			Expect(found).To(BeTrue(), "created policy should appear in list")
		})

		It("deletes the policy", func() {
			Expect(policyID).NotTo(BeEmpty(), "policy must be created first")

			resp, err := doRequest(http.MethodDelete, "/policies/"+policyID, "")
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

			By("confirming the policy is gone")
			deletedID := policyID
			policyID = ""
			resp, err = doRequest(http.MethodGet, "/policies/"+deletedID, "")
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Context("error cases", func() {
		It("returns 404 for a non-existent policy", func() {
			resp, err := doRequest(http.MethodGet, "/policies/does-not-exist", "")
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})
})
