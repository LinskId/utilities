//go:build e2e

package e2e_test

import (
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Storage SP Status Events", Label("sp", "storage", "nats", "tier-a-only"), func() {
	BeforeEach(func() {
		requireStorageSP()
	})

	Context("CloudEvent format", Ordered, func() {
		var collector *NATSCollector
		var volumeID string

		BeforeAll(func() {
			collector = newNATSCollector(natsStorageSubject)
		})

		AfterAll(func() {
			collector.Close()
			if volumeID != "" {
				deleteTestVolume(volumeID)
			}
		})

		It("publishes a CloudEvent on volume creation", func() {
			name := uniqueName("e2e-ce")
			body := createTestVolume(volumeSpec(name, defaultTestCapacity))
			volumeID = body["id"].(string)

			Eventually(func() int {
				return len(collector.EventsForInstance(volumeID))
			}).WithTimeout(30 * time.Second).WithPolling(1 * time.Second).Should(BeNumerically(">", 0),
				"expected at least one NATS event for volume %s", volumeID)

			events := collector.EventsForInstance(volumeID)
			evt := events[0]

			By("validating CloudEvent required fields")
			Expect(evt.SpecVersion).To(Equal("1.0"))
			Expect(evt.ID).NotTo(BeEmpty())
			Expect(evt.Source).To(HavePrefix("dcm/providers/"), "source should be dcm/providers/<providerName>")
			Expect(evt.Type).NotTo(BeEmpty())
			Expect(evt.Time).NotTo(BeEmpty())
			Expect(evt.DataContentType).To(Equal("application/json"))
		})
	})

	Context("healthy volume lifecycle", Ordered, func() {
		var collector *NATSCollector
		var volumeID string

		BeforeAll(func() {
			collector = newNATSCollector(natsStorageSubject)
		})

		AfterAll(func() {
			collector.Close()
			if volumeID != "" {
				deleteTestVolume(volumeID)
			}
		})

		It("transitions to RUNNING and emits status events", func() {
			name := uniqueName("e2e-run")
			body := createTestVolume(volumeSpec(name, defaultTestCapacity))
			volumeID = body["id"].(string)

			By("waiting for RUNNING status via NATS")
			evt := collector.WaitForStatus(volumeID, "RUNNING", 120*time.Second)
			Expect(evt.Data["status"]).To(Equal("RUNNING"))
			Expect(evt.Data["id"]).To(Equal(volumeID))

			By("confirming GET also shows RUNNING")
			resp, err := doStorageSPRequest(http.MethodGet, "/volumes/"+volumeID, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var getBody map[string]interface{}
			decodeJSON(resp, &getBody)
			Expect(getBody["status"]).To(Equal("RUNNING"))
		})

		It("has a valid status progression ending in RUNNING", func() {
			events := collector.EventsForInstance(volumeID)
			Expect(events).NotTo(BeEmpty())

			validStatuses := map[string]bool{"PROVISIONING": true, "RUNNING": true}
			for _, e := range events {
				s, _ := e.Data["status"].(string)
				Expect(validStatuses).To(HaveKey(s), "unexpected status %q in event history", s)
			}
			last := events[len(events)-1]
			Expect(last.Data["status"]).To(Equal("RUNNING"))
		})
	})

	Context("first observed status", Ordered, func() {
		var collector *NATSCollector
		var volumeID string

		BeforeAll(func() {
			collector = newNATSCollector(natsStorageSubject)
		})

		AfterAll(func() {
			collector.Close()
			if volumeID != "" {
				deleteTestVolume(volumeID)
			}
		})

		It("emits PROVISIONING or RUNNING as the first observed status", func() {
			name := uniqueName("e2e-init")
			body := createTestVolume(volumeSpec(name, defaultTestCapacity))
			volumeID = body["id"].(string)

			Eventually(func() int {
				return len(collector.EventsForInstance(volumeID))
			}).WithTimeout(15 * time.Second).WithPolling(1 * time.Second).Should(BeNumerically(">", 0))

			events := collector.EventsForInstance(volumeID)
			first := events[0].Data["status"].(string)
			Expect(first).To(SatisfyAny(Equal("PROVISIONING"), Equal("RUNNING")),
				"first event should be either PROVISIONING or RUNNING, got %q", first)
		})
	})

	Context("delete status event", Ordered, func() {
		var collector *NATSCollector
		var volumeID string

		BeforeAll(func() {
			collector = newNATSCollector(natsStorageSubject)
		})

		AfterAll(func() {
			collector.Close()
		})

		It("creates a volume, waits for RUNNING, then deletes", func() {
			name := uniqueName("e2e-del")
			body := createTestVolume(volumeSpec(name, defaultTestCapacity))
			volumeID = body["id"].(string)

			collector.WaitForStatus(volumeID, "RUNNING", 120*time.Second)

			resp, err := doStorageSPRequest(http.MethodDelete, "/volumes/"+volumeID, "")
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

			By("waiting for a DELETED status event")
			Eventually(func() bool {
				for _, e := range collector.EventsForInstance(volumeID) {
					s, _ := e.Data["status"].(string)
					if s == "DELETED" {
						return true
					}
				}
				return false
			}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(BeTrue(),
				"expected a DELETED status event after deletion")
		})
	})

	Context("label filtering", Label("cluster"), Ordered, func() {
		var collector *NATSCollector
		manualPVCName := uniqueName("e2e-unlbl")

		BeforeAll(func() {
			requireKubectl()
			collector = newNATSCollector(natsStorageSubject)
		})

		AfterAll(func() {
			collector.Close()
			_, _ = runStorageKubectl("delete", "pvc", manualPVCName, "--ignore-not-found")
		})

		It("does not emit events for non-DCM PVCs", func() {
			By("creating a PVC without DCM labels via kubectl")
			manifest := `apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: ` + manualPVCName + `
spec:
  accessModes:
    - ReadWriteOnce
  storageClassName: ` + testStorageClass() + `
  resources:
    requests:
      storage: 1Gi
`
			err := applyStorageManifest(manifest)
			Expect(err).NotTo(HaveOccurred())

			By("waiting and confirming no events appear for the manual PVC")
			Consistently(func() int {
				return len(collector.EventsForInstance(manualPVCName))
			}).WithTimeout(15 * time.Second).WithPolling(2 * time.Second).Should(Equal(0),
				"no NATS events should be emitted for non-DCM PVC %s", manualPVCName)
		})
	})

	Context("concurrent volumes", Ordered, func() {
		var collector *NATSCollector
		var volumeIDs []string

		BeforeAll(func() {
			collector = newNATSCollector(natsStorageSubject)
		})

		AfterAll(func() {
			collector.Close()
			for _, id := range volumeIDs {
				deleteTestVolume(id)
			}
		})

		It("emits independent event streams for each volume", func() {
			const count = 3
			for i := 0; i < count; i++ {
				name := uniqueName("e2e-con")
				body := createTestVolume(volumeSpec(name, defaultTestCapacity))
				volumeIDs = append(volumeIDs, body["id"].(string))
			}

			for _, id := range volumeIDs {
				Eventually(func() int {
					return len(collector.EventsForInstance(id))
				}).WithTimeout(120 * time.Second).WithPolling(2 * time.Second).Should(BeNumerically(">", 0),
					"expected at least one event for volume %s", id)
			}

			By("verifying events are isolated per instance")
			for _, id := range volumeIDs {
				events := collector.EventsForInstance(id)
				for _, e := range events {
					Expect(e.Data["id"]).To(Equal(id),
						"event for volume %s contains wrong instance ID", id)
				}
			}
		})
	})
})
