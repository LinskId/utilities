//go:build e2e

package e2e_test

import (
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Storage SP API", Label("sp", "storage", "tier-a-only"), func() {
	BeforeEach(func() {
		requireStorageSP()
	})

	Context("health", func() {
		It("returns healthy status", func() {
			resp, err := doStorageSPRequest(http.MethodGet, "/volumes/health", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body map[string]interface{}
			decodeJSON(resp, &body)
			Expect(body["status"]).To(Equal("healthy"))
			Expect(body).To(HaveKey("uptime"))
			Expect(body).To(HaveKey("version"))
			Expect(body).To(HaveKey("type"))
		})
	})

	Context("CRUD lifecycle", Ordered, func() {
		var volumeIDs []string

		AfterAll(func() {
			for _, id := range volumeIDs {
				deleteTestVolume(id)
			}
		})

		It("creates a volume with valid spec", func() {
			name := uniqueName("e2e-vol")
			body := createTestVolume(volumeSpec(name, defaultTestCapacity))

			id := body["id"].(string)
			Expect(id).To(Equal(name))
			volumeIDs = append(volumeIDs, id)
		})

		It("creates a volume with a client-assigned id query parameter", func() {
			customID := uniqueName("e2e-custom")
			placeholderName := uniqueName("e2e-placeholder")
			resp, err := doStorageSPRequest(http.MethodPost,
				"/volumes?id="+customID,
				volumeSpec(placeholderName, defaultTestCapacity))
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusCreated))

			var body map[string]interface{}
			decodeJSON(resp, &body)
			Expect(body["id"]).To(Equal(customID))

			spec, ok := body["spec"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			meta, ok := spec["metadata"].(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(meta["name"]).To(Equal(customID),
				"SP should canonicalize metadata.name to the id query parameter")

			volumeIDs = append(volumeIDs, customID)
		})

		It("derives the id from metadata.name when no query parameter is provided", func() {
			name := uniqueName("e2e-autoid")
			body := createTestVolume(volumeSpec(name, defaultTestCapacity))

			id := body["id"].(string)
			Expect(id).To(Equal(name))
			volumeIDs = append(volumeIDs, id)
		})

		It("gets an existing volume by ID", func() {
			Expect(volumeIDs).NotTo(BeEmpty())
			id := volumeIDs[0]

			Eventually(func() string {
				resp, err := doStorageSPRequest(http.MethodGet, "/volumes/"+id, "")
				Expect(err).NotTo(HaveOccurred())
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				var body map[string]interface{}
				decodeJSON(resp, &body)
				s, _ := body["status"].(string)
				return s
			}).WithTimeout(120 * time.Second).WithPolling(3 * time.Second).Should(Equal("RUNNING"),
				"volume should reach RUNNING status when PVC is bound")
		})

		It("lists all managed volumes", func() {
			resp, err := doStorageSPRequest(http.MethodGet, "/volumes", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body map[string]interface{}
			decodeJSON(resp, &body)
			Expect(body).To(HaveKey("volumes"))

			volumes, ok := body["volumes"].([]interface{})
			Expect(ok).To(BeTrue())
			Expect(len(volumes)).To(BeNumerically(">=", len(volumeIDs)),
				"list should include at least all volumes we created")
		})

		It("paginates volume list", func() {
			resp, err := doStorageSPRequest(http.MethodGet, "/volumes?max_page_size=2", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var page1 map[string]interface{}
			decodeJSON(resp, &page1)
			volumes := page1["volumes"].([]interface{})
			Expect(len(volumes)).To(BeNumerically("<=", 2))

			if token, ok := page1["next_page_token"].(string); ok && token != "" {
				resp2, err := doStorageSPRequest(http.MethodGet,
					"/volumes?max_page_size=2&page_token="+token, "")
				Expect(err).NotTo(HaveOccurred())
				Expect(resp2.StatusCode).To(Equal(http.StatusOK))

				var page2 map[string]interface{}
				decodeJSON(resp2, &page2)
				Expect(page2).To(HaveKey("volumes"))
			}
		})
	})

	Context("validation errors", func() {
		It("rejects an empty body", func() {
			resp, err := doStorageSPRequest(http.MethodPost, "/volumes", "")
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("rejects a body with missing required fields", func() {
			resp, err := doStorageSPRequest(http.MethodPost, "/volumes", `{"spec":{}}`)
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("rejects invalid field types", func() {
			resp, err := doStorageSPRequest(http.MethodPost, "/volumes", `{"spec": {"service_type": 12345}}`)
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("rejects the reserved volume id health", func() {
			resp, err := doStorageSPRequest(http.MethodPost, "/volumes?id=health", volumeSpec("health", defaultTestCapacity))
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("rejects a non-existent StorageClass", func() {
			name := uniqueName("e2e-badsc")
			resp, err := doStorageSPRequest(http.MethodPost, "/volumes",
				volumeSpecWith(name, defaultTestCapacity, "does-not-exist-sc"))
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("rejects wrong content type", func() {
			url := storageSPBaseURL + "/volumes"
			req, err := http.NewRequest(http.MethodPost, url, nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Content-Type", "text/plain")

			resp, err := httpClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))
		})

		It("returns 409 when creating the same volume twice", func() {
			name := uniqueName("e2e-dup")
			createTestVolume(volumeSpec(name, defaultTestCapacity))
			defer deleteTestVolume(name)

			resp, err := doStorageSPRequest(http.MethodPost, "/volumes", volumeSpec(name, defaultTestCapacity))
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		})
	})

	Context("not found", func() {
		It("returns 404 for GET on non-existent volume", func() {
			resp, err := doStorageSPRequest(http.MethodGet, "/volumes/does-not-exist", "")
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("returns 404 for DELETE on non-existent volume", func() {
			resp, err := doStorageSPRequest(http.MethodDelete, "/volumes/does-not-exist", "")
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})

	Context("label filtering", Label("cluster"), Ordered, func() {
		manualPVCName := uniqueName("e2e-manual")

		BeforeAll(func() {
			requireKubectl()
		})

		AfterAll(func() {
			_, _ = runStorageKubectl("delete", "pvc", manualPVCName, "--ignore-not-found")
		})

		It("excludes non-DCM PVCs from list", func() {
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

			By("listing volumes via the SP API")
			resp, err := doStorageSPRequest(http.MethodGet, "/volumes", "")
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body map[string]interface{}
			decodeJSON(resp, &body)
			volumes, _ := body["volumes"].([]interface{})

			for _, v := range volumes {
				vol := v.(map[string]interface{})
				id, _ := vol["id"].(string)
				Expect(id).NotTo(Equal(manualPVCName),
					"non-DCM PVC %q should not appear in SP list", manualPVCName)
			}
		})
	})

	Context("delete lifecycle", Ordered, func() {
		var deleteID string

		It("creates a volume to delete", func() {
			name := uniqueName("e2e-delete")
			body := createTestVolume(volumeSpec(name, defaultTestCapacity))
			deleteID = body["id"].(string)
		})

		It("deletes the volume and its PVC", func() {
			Expect(deleteID).NotTo(BeEmpty())

			resp, err := doStorageSPRequest(http.MethodDelete, "/volumes/"+deleteID, "")
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))

			By("confirming the volume is gone")
			resp, err = doStorageSPRequest(http.MethodGet, "/volumes/"+deleteID, "")
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})
	})
})
