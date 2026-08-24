//go:build e2e

package e2e_test

import (
	"fmt"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Agents API", func() {
	// No AfterAll cleanup: the agents API has no DELETE endpoint — agents
	// auto-deregister via heartbeat timeout. We use a synthetic service type
	// ("e2e-test-type") so stale agents never interfere with real discovery.
	Context("registration lifecycle", Ordered, func() {
		var agentID string
		agentName := fmt.Sprintf("e2e-test-agent-%d", time.Now().UnixNano())

		It("registers an agent", func() {
			payload := fmt.Sprintf(`{
				"name": %q,
				"environment": "e2e-test",
				"topic_name": "dcm.agent.%s",
				"service_types": ["e2e-test-type"],
				"cost": "low"
			}`, agentName, agentName)

			resp, err := doRequest(http.MethodPost, "/agents", payload)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))

			var body map[string]interface{}
			decodeJSON(resp, &body)
			GinkgoWriter.Printf("Create agent response: %v\n", body)
			Expect(body).To(HaveKey("agent_id"))
			Expect(body["name"]).To(Equal(agentName))

			id, ok := body["agent_id"].(string)
			Expect(ok).To(BeTrue(), "agent_id should be a string")
			agentID = id
		})

		It("gets the agent by ID", func() {
			Expect(agentID).NotTo(BeEmpty(), "agent must be registered first")

			resp, err := doRequest(http.MethodGet, "/agents/"+agentID, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body map[string]interface{}
			decodeJSON(resp, &body)
			Expect(body["name"]).To(Equal(agentName))
			Expect(body["environment"]).To(Equal("e2e-test"))
		})

		It("lists agents and includes the registered one", func() {
			Expect(agentID).NotTo(BeEmpty(), "agent must be registered first")

			resp, err := doRequest(http.MethodGet, "/agents", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body map[string]interface{}
			decodeJSON(resp, &body)
			Expect(body).To(HaveKey("agents"))

			agents, ok := body["agents"].([]interface{})
			Expect(ok).To(BeTrue())

			var found bool
			for _, a := range agents {
				agent, ok := a.(map[string]interface{})
				Expect(ok).To(BeTrue(), "agent entry should be a map")
				if agent["agent_id"] == agentID {
					found = true
					break
				}
			}
			Expect(found).To(BeTrue(), "registered agent should appear in list")
		})

		It("re-registers the agent (idempotent by name)", func() {
			Expect(agentID).NotTo(BeEmpty(), "agent must be registered first")

			payload := fmt.Sprintf(`{
				"name": %q,
				"environment": "e2e-test-updated",
				"topic_name": "dcm.agent.%s",
				"service_types": ["e2e-test-type", "e2e-test-type-2"],
				"cost": "medium"
			}`, agentName, agentName)

			resp, err := doRequest(http.MethodPost, "/agents", payload)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body map[string]interface{}
			decodeJSON(resp, &body)
			Expect(body["agent_id"]).To(Equal(agentID), "re-registration should return the same agent_id")
			Expect(body["environment"]).To(Equal("e2e-test-updated"))
		})
	})

	Context("error cases", func() {
		It("returns 404 for a non-existent agent", func() {
			resp, err := doRequest(http.MethodGet, "/agents/does-not-exist", "")
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})
})
