package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/storage"
)

func TestRewardRequestValidationBeforeDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{}
	for _, tc := range []struct {
		name, body string
		handler    gin.HandlerFunc
	}{
		{"decimal exchange", `{"units":1.5}`, s.exchangeReward},
		{"overflow exchange", `{"units":999999999999999999999}`, s.exchangeReward},
		{"decimal exchange limit", `{"max_redemptions_per_user":1.5}`, s.saveExchangeOffer},
		{"overflow exchange limit", `{"max_redemptions_per_user":999999999999999999999}`, s.saveExchangeOffer},
		{"old asset field", `{"cost_definition_id":"old"}`, s.saveExchangeOffer},
		{"old claim field", `{"delivery_mode":"claim"}`, s.createAdminRewardGrant},
		{"decimal grant", `{"quantity":1.5}`, s.createAdminRewardGrant},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Params = gin.Params{{Key: "id", Value: uuid.NewString()}}
			c.Request = httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			tc.handler(c)
			if w.Code != 400 {
				t.Fatalf("status=%d", w.Code)
			}
		})
	}
}

func TestRewardErrorsExposeStableCodes(t *testing.T) {
	for _, tc := range []struct {
		err    error
		code   string
		status int
	}{
		{storage.ErrExchangeChanged, "exchange_revision_conflict", 409}, {storage.ErrRewardKey, "invalid_reward_key", 400},
		{storage.ErrExchangeUnavailable, "exchange_offer_disabled", 400}, {storage.ErrExchangeStock, "exchange_stock_insufficient", 409}, {storage.ErrPointsCDKSameBatch, "points_cdk_batch_redeemed", 409}, {storage.ErrInsufficientReward, "insufficient_balance", 400},
		{storage.ErrExchangeLimitReached, "exchange_limit_reached", 409}, {storage.ErrRewardRequestConflict, "request_conflict", 409}, {storage.ErrRewardAdminRequired, "admin_required", 403},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		rewardFail(c, tc.err)
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != tc.status || body["code"] != tc.code {
			t.Fatalf("error %v: %d %v", tc.err, w.Code, body)
		}
	}
}

func TestRewardPrivateFieldsOnlyInAdminView(t *testing.T) {
	actor, request := uuid.New(), uuid.New()
	row := storage.RewardLedgerEntry{ID: uuid.New(), PlayerID: uuid.New(), ActorID: &actor, RequestID: &request, SourceID: request.String(), Reason: "private_reason", RequestHash: "private_hash"}
	public, _ := json.Marshal(row)
	for _, key := range []string{"private_reason", "private_hash", "user_id", "operator_id", "request_id", request.String(), actor.String()} {
		if strings.Contains(string(public), key) {
			t.Fatalf("private field leaked: %s", key)
		}
	}
	admin, _ := json.Marshal(row.AdminView())
	if !strings.Contains(string(admin), "private_reason") || !strings.Contains(string(admin), "operator_id") {
		t.Fatal("admin response missing trace fields")
	}
}

func TestRewardDuplicateCDKListsCodes(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	rewardFail(c, &storage.ExchangeDuplicateError{Codes: []string{"A", "B"}})
	var body struct {
		Code       string   `json:"code"`
		Duplicates []string `json:"duplicates"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 400 || body.Code != "exchange_cdk_duplicate" || len(body.Duplicates) != 2 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
