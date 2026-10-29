package mtn

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/light-group/light-ex-backend/internal/modules/payment/provider"
)

func TestParseCallback_Success(t *testing.T) {
	jsonPayload := `{
		"financialTransactionId": "ft-998877",
		"externalId": "DEP-100",
		"amount": "100.00",
		"currency": "GHS",
		"status": "SUCCESSFUL",
		"payer": {
			"partyIdType": "MSISDN",
			"partyId": "233240000000"
		}
	}`

	notif, err := ParseCallback([]byte(jsonPayload))
	if err != nil {
		t.Fatalf("unexpected error parsing callback: %v", err)
	}

	if notif.FinancialTransactionID != "ft-998877" {
		t.Errorf("expected financialTransactionId ft-998877, got %s", notif.FinancialTransactionID)
	}
	if notif.Status != "SUCCESSFUL" {
		t.Errorf("expected status SUCCESSFUL, got %s", notif.Status)
	}

	result := notif.Result()
	if result.Status != provider.StatusSucceeded {
		t.Errorf("expected StatusSucceeded, got %v", result.Status)
	}
	if result.ProviderReference != "ft-998877" {
		t.Errorf("expected ProviderReference ft-998877, got %s", result.ProviderReference)
	}
}

func TestParseCallback_Invalid(t *testing.T) {
	_, err := ParseCallback([]byte(""))
	if err == nil {
		t.Errorf("expected error on empty payload")
	}

	_, err = ParseCallback([]byte("not-json"))
	if err == nil {
		t.Errorf("expected error on non-json payload")
	}
}

func TestWebhookHandler_Integration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var receivedNotif *CallbackNotification
	processor := func(c *gin.Context, notif *CallbackNotification) error {
		receivedNotif = notif
		return nil
	}

	r := gin.New()
	r.POST("/api/v1/payments/webhook/mtn", WebhookHandler(processor))

	payload := `{"financialTransactionId":"fin-1","status":"SUCCESSFUL","externalId":"REF-1"}`
	req, _ := http.NewRequest(http.MethodPost, "/api/v1/payments/webhook/mtn", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}
	if receivedNotif == nil || receivedNotif.FinancialTransactionID != "fin-1" {
		t.Fatalf("expected processor to receive notification, got %+v", receivedNotif)
	}
}
