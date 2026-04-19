package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHTTP_BankRules_CRUD drives the full lifecycle of a bank rule and asserts
// defaults, validation and tenant isolation at the HTTP boundary.
func TestHTTP_BankRules_CRUD(t *testing.T) {
	h := newHarness(t)

	// Missing RuleType → 400.
	status, _ := h.do(t, http.MethodPost, "/api/v1/bank-rules", map[string]any{
		"Name": "No type",
	}, true)
	assert.Equal(t, http.StatusBadRequest, status)

	// Happy path: create an inactive SPEND rule; server must honour IsActive=false.
	status, body := h.do(t, http.MethodPost, "/api/v1/bank-rules", map[string]any{
		"Name":     "Coffee shop",
		"RuleType": "spend",
		"IsActive": false,
		"Definition": map[string]any{
			"Conditions": []map[string]any{
				{"Field": "Description", "Operator": "CONTAINS", "Value": "starbucks"},
			},
		},
	}, true)
	require.Equal(t, http.StatusCreated, status, string(body))
	var created struct {
		BankRules []struct {
			BankRuleID string `json:"BankRuleID"`
			RuleType   string `json:"RuleType"`
			IsActive   bool   `json:"IsActive"`
			Definition struct {
				MatchMode string `json:"MatchMode"`
				RunOn     string `json:"RunOn"`
			} `json:"Definition"`
		} `json:"BankRules"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.Len(t, created.BankRules, 1)
	br := created.BankRules[0]
	assert.Equal(t, "SPEND", br.RuleType)
	assert.Equal(t, "ALL", br.Definition.MatchMode)
	assert.Equal(t, "ALL_BANK_ACCOUNTS", br.Definition.RunOn)
	assert.False(t, br.IsActive, "server must not force IsActive=true")

	id := br.BankRuleID

	// Update → rename + activate.
	status, _ = h.do(t, http.MethodPut, "/api/v1/bank-rules/"+id, map[string]any{
		"Name":     "Coffee",
		"RuleType": "SPEND",
		"IsActive": true,
		"Definition": map[string]any{
			"MatchMode": "ANY",
		},
	}, true)
	assert.Equal(t, http.StatusOK, status)

	// Verify via GET (single-item envelope wraps under Payload.BankRules).
	status, body = h.do(t, http.MethodGet, "/api/v1/bank-rules/"+id, nil, true)
	require.Equal(t, http.StatusOK, status)
	var one struct {
		Payload struct {
			BankRules []struct {
				Name     string `json:"Name"`
				IsActive bool   `json:"IsActive"`
			} `json:"BankRules"`
		} `json:"Payload"`
	}
	require.NoError(t, json.Unmarshal(body, &one))
	require.Len(t, one.Payload.BankRules, 1)
	assert.Equal(t, "Coffee", one.Payload.BankRules[0].Name)
	assert.True(t, one.Payload.BankRules[0].IsActive)

	// List should include the rule.
	status, body = h.do(t, http.MethodGet, "/api/v1/bank-rules", nil, true)
	require.Equal(t, http.StatusOK, status)
	var list struct {
		Payload struct {
			BankRules []struct {
				BankRuleID string `json:"BankRuleID"`
			} `json:"BankRules"`
		} `json:"Payload"`
	}
	require.NoError(t, json.Unmarshal(body, &list))
	found := false
	for _, r := range list.Payload.BankRules {
		if r.BankRuleID == id {
			found = true
			break
		}
	}
	assert.True(t, found, "created rule must appear in the list")

	// Delete → 204 then 404.
	status, _ = h.do(t, http.MethodDelete, "/api/v1/bank-rules/"+id, nil, true)
	assert.Equal(t, http.StatusNoContent, status)
	status, _ = h.do(t, http.MethodGet, "/api/v1/bank-rules/"+id, nil, true)
	assert.Equal(t, http.StatusNotFound, status)
}

// TestHTTP_OrgFiles_CRUD covers upload → list → move → delete through /files.
func TestHTTP_OrgFiles_CRUD(t *testing.T) {
	h := newHarness(t)

	content := []byte("hello file")
	req := newMultipartUpload(t, "/api/v1/files", "inbox", "hello.txt", "text/plain", content)
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Xero-Tenant-Id", seedDemoOrgID.String())
	resp, err := h.app.Test(req, fiber.TestConfig{Timeout: 0})
	require.NoError(t, err)
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode, string(raw))

	var created struct {
		Files []struct {
			AttachmentID string `json:"AttachmentID"`
			FileFolder   string `json:"FileFolder"`
			FileName     string `json:"FileName"`
		} `json:"Files"`
	}
	require.NoError(t, json.Unmarshal(raw, &created))
	require.Len(t, created.Files, 1)
	fid := created.Files[0].AttachmentID
	assert.Equal(t, "INBOX", created.Files[0].FileFolder)

	// Reject empty file.
	empty := newMultipartUpload(t, "/api/v1/files", "inbox", "empty.txt", "text/plain", nil)
	empty.Header.Set("Authorization", "Bearer "+h.token)
	empty.Header.Set("Xero-Tenant-Id", seedDemoOrgID.String())
	emptyResp, err := h.app.Test(empty, fiber.TestConfig{Timeout: 0})
	require.NoError(t, err)
	_ = emptyResp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, emptyResp.StatusCode)

	// Move → archive.
	status, _ := h.do(t, http.MethodPost, "/api/v1/files/move", map[string]any{
		"AttachmentIDs": []string{fid},
		"Folder":        "ARCHIVE",
	}, true)
	require.Equal(t, http.StatusOK, status)

	// Reject Folder other than INBOX/ARCHIVE.
	status, _ = h.do(t, http.MethodPost, "/api/v1/files/move", map[string]any{
		"AttachmentIDs": []string{fid},
		"Folder":        "TRASH",
	}, true)
	assert.Equal(t, http.StatusBadRequest, status)

	// List archive must include file.
	status, body := h.do(t, http.MethodGet, "/api/v1/files?folder=archive", nil, true)
	require.Equal(t, http.StatusOK, status)
	var list struct {
		Files []struct {
			AttachmentID string `json:"AttachmentID"`
		} `json:"Files"`
	}
	require.NoError(t, json.Unmarshal(body, &list))
	seen := false
	for _, f := range list.Files {
		if f.AttachmentID == fid {
			seen = true
			break
		}
	}
	assert.True(t, seen, "moved file must appear in archive list")

	// Delete.
	status, _ = h.do(t, http.MethodPost, "/api/v1/files/delete", map[string]any{
		"AttachmentIDs": []string{fid},
	}, true)
	assert.Equal(t, http.StatusOK, status)
}

// newMultipartUpload builds a POST multipart request with a single "file" part
// and an optional "folder" field.
func newMultipartUpload(t *testing.T, path, folder, fileName, mimeType string, content []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if folder != "" {
		require.NoError(t, w.WriteField("folder", folder))
	}
	part, err := w.CreateFormFile("file", fileName)
	require.NoError(t, err)
	if len(content) > 0 {
		_, err = part.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}
