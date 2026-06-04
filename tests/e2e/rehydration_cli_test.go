//go:build e2e

package e2e_test

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Rehydration CLI", Label("rehydration", "cli"), func() {
	BeforeEach(func() {
		requireCLI()
		requireThreeTierSP()
	})

	It("dcm catalog instance rehydrate succeeds for valid instance", func() {
		provider := threeTierProviders[0]
		policyID := createPlacementPolicy("tc40-policy", regoSelectProvider(provider.Name))
		defer deletePlacementPolicy(policyID)

		inst := createTestInstance(uniqueName("tc40"), defaultUserValues())
		defer deleteInstance(inst.UID)

		waitForInstanceRunning(inst.ResourceID, provisionTimeout)

		stdout, stderr, exitCode := runDCM("catalog", "instance", "rehydrate", inst.UID)

		Expect(exitCode).To(Equal(0),
			"expected exit 0, got %d\nstdout: %s\nstderr: %s", exitCode, stdout, stderr)

		Expect(stdout).To(SatisfyAny(
			ContainSubstring("resource_id"),
			ContainSubstring(inst.UID),
		), "stdout should contain resource_id or instance UID")
	})

	It("dcm catalog instance rehydrate fails for non-existent instance", func() {
		fakeUID := "does-not-exist-" + uniqueName("tc41")
		stdout, stderr, exitCode := runDCM("catalog", "instance", "rehydrate", fakeUID)

		Expect(exitCode).NotTo(Equal(0),
			"expected non-zero exit code\nstdout: %s\nstderr: %s", stdout, stderr)

		combined := stdout + stderr
		Expect(strings.ToLower(combined)).To(SatisfyAny(
			ContainSubstring("not found"),
			ContainSubstring("error"),
			ContainSubstring("404"),
		), "output should indicate an error")
	})
})
