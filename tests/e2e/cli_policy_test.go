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

var _ = Describe("CLI: policy commands", Label("cli"), func() {
	Context("CRUD lifecycle", Ordered, func() {
		BeforeAll(func() { requireCLI() })

		var policyID string
		policyDisplayName := fmt.Sprintf("E2E Test Policy %d", time.Now().UnixNano())

		policyYAML := fmt.Sprintf(`display_name: %s
policy_type: GLOBAL
priority: 100
description: Created by E2E test suite
rego_code: |
  package authz

  main := {
    "rejected": false
  }
`, policyDisplayName)

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

		It("creates a policy from YAML file", func() {
			yamlFile := writeTempFile(policyYAML, ".yaml")

			stdout, stderr, exitCode := runDCM("policy", "create", "--from-file", yamlFile, "--output", "json")

			Expect(exitCode).To(Equal(0), "stderr: %s", stderr)

			var result map[string]interface{}
			Expect(json.Unmarshal([]byte(stdout), &result)).To(Succeed())
			Expect(result).To(HaveKey("id"))

			id, ok := result["id"].(string)
			Expect(ok).To(BeTrue(), "id should be a string")
			Expect(id).NotTo(BeEmpty())
			policyID = id
		})

		It("gets the policy by ID", func() {
			Expect(policyID).NotTo(BeEmpty(), "policy must be created first")

			stdout, stderr, exitCode := runDCM("policy", "get", policyID)

			Expect(exitCode).To(Equal(0), "stderr: %s", stderr)
			Expect(stdout).To(ContainSubstring(policyDisplayName))
		})

		It("lists policies and includes the created one", func() {
			Expect(policyID).NotTo(BeEmpty(), "policy must be created first")

			stdout, stderr, exitCode := runDCM("policy", "list")

			Expect(exitCode).To(Equal(0), "stderr: %s", stderr)
			Expect(stdout).To(ContainSubstring(policyID))
		})

		It("lists policies in JSON format", func() {
			Expect(policyID).NotTo(BeEmpty(), "policy must be created first")

			stdout, stderr, exitCode := runDCM("policy", "list", "--output", "json")

			Expect(exitCode).To(Equal(0), "stderr: %s", stderr)

			var result map[string]interface{}
			Expect(json.Unmarshal([]byte(stdout), &result)).To(Succeed())
			Expect(result).To(HaveKey("results"))
		})

		It("deletes the policy", func() {
			Expect(policyID).NotTo(BeEmpty(), "policy must be created first")

			_, stderr, exitCode := runDCM("policy", "delete", policyID)

			Expect(exitCode).To(Equal(0), "stderr: %s", stderr)
			policyID = ""
		})
	})

	Context("error handling", func() {
		It("returns exit code 1 for non-existent policy", func() {
			_, stderr, exitCode := runDCM("policy", "get", "does-not-exist")

			Expect(exitCode).To(Equal(1))
			Expect(stderr).NotTo(BeEmpty())
		})

		It("returns exit code 2 for missing required flags", func() {
			_, stderr, exitCode := runDCM("policy", "create")

			Expect(exitCode).To(Equal(2))
			Expect(stderr).To(ContainSubstring("from-file"))
		})
	})
})
