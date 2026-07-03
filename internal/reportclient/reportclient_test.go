package reportclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"undump/internal/models"
	"undump/internal/reportclient"
)

func TestSend_SuccessSendsAuthAndBody(t *testing.T) {
	var gotAuth, gotPath string
	var gotBody models.RunReport

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	report := models.RunReport{TargetName: "prod-billing", Status: models.StatusPass, Checks: []models.CheckResult{}}
	ok := reportclient.Send(context.Background(), server.URL, "test-api-key", report)

	assert.True(t, ok)
	assert.Equal(t, "Bearer test-api-key", gotAuth)
	assert.Equal(t, "/v1/runs", gotPath)
	assert.Equal(t, "prod-billing", gotBody.TargetName)
}

func TestSend_ServerErrorReturnsFalseWithoutPanic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ok := reportclient.Send(context.Background(), server.URL, "key", models.RunReport{Checks: []models.CheckResult{}})
	assert.False(t, ok)
}

func TestSend_UnreachableCloudReturnsFalseWithoutPanic(t *testing.T) {
	ok := reportclient.Send(context.Background(), "http://127.0.0.1:1", "key", models.RunReport{Checks: []models.CheckResult{}})
	assert.False(t, ok)
}
