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
	_, ok := reportclient.Send(context.Background(), server.URL, "test-api-key", report)

	assert.True(t, ok)
	assert.Equal(t, "Bearer test-api-key", gotAuth)
	assert.Equal(t, "/v1/runs", gotPath)
	assert.Equal(t, "prod-billing", gotBody.TargetName)
}

func TestSend_ParsesLastRowcountFromResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id": 42, "last_rowcount": 1000}`))
	}))
	defer server.Close()

	resp, ok := reportclient.Send(context.Background(), server.URL, "key", models.RunReport{Checks: []models.CheckResult{}})

	require.True(t, ok)
	assert.Equal(t, int64(42), resp.RunID)
	require.NotNil(t, resp.LastRowcount)
	assert.Equal(t, int64(1000), *resp.LastRowcount)
}

func TestSend_NullLastRowcountIsNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id": 42, "last_rowcount": null}`))
	}))
	defer server.Close()

	resp, ok := reportclient.Send(context.Background(), server.URL, "key", models.RunReport{Checks: []models.CheckResult{}})

	require.True(t, ok)
	assert.Nil(t, resp.LastRowcount)
}

func TestSend_UnparsableBodyStillReportsDelivered(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	resp, ok := reportclient.Send(context.Background(), server.URL, "key", models.RunReport{Checks: []models.CheckResult{}})

	assert.True(t, ok)
	assert.Nil(t, resp.LastRowcount)
}

func TestSend_ServerErrorReturnsFalseWithoutPanic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, ok := reportclient.Send(context.Background(), server.URL, "key", models.RunReport{Checks: []models.CheckResult{}})
	assert.False(t, ok)
}

func TestSend_UnreachableCloudReturnsFalseWithoutPanic(t *testing.T) {
	_, ok := reportclient.Send(context.Background(), "http://127.0.0.1:1", "key", models.RunReport{Checks: []models.CheckResult{}})
	assert.False(t, ok)
}
