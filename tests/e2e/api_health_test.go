//go:build e2e

package e2e_test

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Health Endpoints", Label("smoke"), func() {
	It("reports healthy control-plane", func() {
		resp, err := doRequest(http.MethodGet, "/health", "")
		Expect(err).NotTo(HaveOccurred())

		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		var body map[string]interface{}
		decodeJSON(resp, &body)
		Expect(body).To(HaveKeyWithValue("status", "ok"))
		Expect(body).To(HaveKeyWithValue("path", "/api/v1alpha1/health"))
	})
})
