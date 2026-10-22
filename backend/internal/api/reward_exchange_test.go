package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/testdb"
)

func TestPostgresExchangeOfferLimitViewsAndErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testdb.Open(t, "api_reward_exchange_limit")
	ctx := context.Background()
	actor := storage.User{ID: uuid.New(), Username: "admin", DisplayName: "管理员", Role: storage.UserRoleAdmin, Status: storage.UserStatusActive}
	player := storage.User{ID: uuid.New(), Username: "player", DisplayName: "玩家", Role: storage.UserRoleUser, Status: storage.UserStatusActive}
	for _, user := range []*storage.User{&actor, &player} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := storage.NewRewardRepository(db)
	server := &Server{rewards: repo}

	save := func(body string) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set(authUserKey, &actor)
		server.saveExchangeOffer(c)
		if w.Code != 200 {
			t.Fatalf("save offer status=%d body=%s", w.Code, w.Body.String())
		}
		var saved map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
			t.Fatal(err)
		}
		return saved
	}

	limited := save(`{"name":"限量商品","description":"","cost_quantity":100,"reward_quantity":1,"max_redemptions_per_user":2,"enabled":true,"sort_order":0}`)
	if limited["max_redemptions_per_user"] != float64(2) {
		t.Fatalf("saved limit = %v", limited["max_redemptions_per_user"])
	}
	unlimited := save(`{"name":"不限商品","description":"","cost_quantity":100,"reward_quantity":1,"enabled":true,"sort_order":1}`)
	if unlimited["max_redemptions_per_user"] != float64(0) {
		t.Fatalf("missing limit should default to unlimited, got %v", unlimited["max_redemptions_per_user"])
	}
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, storage.AdminRewardGrant{
		RequestID: uuid.New(), RewardKey: storage.PointsKey, Quantity: 1000, Reason: "测试", PublicNote: "测试",
	}); err != nil {
		t.Fatal(err)
	}

	offerID := uuid.MustParse(limited["id"].(string))
	revision := int64(limited["revision"].(float64))
	requestID := uuid.New()
	exchange := func(request uuid.UUID, units int64) (int, map[string]any) {
		t.Helper()
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"request_id":"`+request.String()+`","offer_id":"`+offerID.String()+`","revision":`+itoa(revision)+`,"units":`+itoa(units)+`}`))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set(authUserKey, &player)
		server.exchangeReward(c)
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return w.Code, body
	}

	status, order := exchange(requestID, 2)
	if status != 200 {
		t.Fatalf("first exchange status=%d body=%v", status, order)
	}
	orderID := order["id"]

	status, body := exchange(uuid.New(), 1)
	if status != 409 || body["code"] != "exchange_limit_reached" {
		t.Fatalf("over-limit status=%d body=%v", status, body)
	}
	status, replay := exchange(requestID, 2)
	if status != 200 || replay["id"] != orderID {
		t.Fatalf("replay status=%d body=%v", status, replay)
	}

	playerView := offerViews(t, server, &player, false)
	// The baseline already includes one enabled unlimited offer.
	if len(playerView) != 3 {
		t.Fatalf("player offers=%d", len(playerView))
	}
	for _, offer := range playerView {
		switch offer.ID {
		case offerID:
			if offer.MaxRedemptionsPerUser != 2 || offer.RedeemedUnits != 2 || offer.RemainingUnits == nil || *offer.RemainingUnits != 0 {
				t.Fatalf("limited view = %+v", offer)
			}
		default:
			if offer.MaxRedemptionsPerUser != 0 || offer.RedeemedUnits != 0 || offer.RemainingUnits != nil {
				t.Fatalf("unlimited view = %+v", offer)
			}
		}
	}

	adminResponse := listOffersBody(t, server, &actor, true)
	for _, field := range []string{"redeemed_units", "remaining_units"} {
		if strings.Contains(adminResponse, field) {
			t.Fatalf("admin list leaked player field %s: %s", field, adminResponse)
		}
	}
	if !strings.Contains(adminResponse, "max_redemptions_per_user") {
		t.Fatalf("admin list missing limit: %s", adminResponse)
	}
}

type exchangeOfferViewTest struct {
	ID                    uuid.UUID `json:"id"`
	MaxRedemptionsPerUser int64     `json:"max_redemptions_per_user"`
	RedeemedUnits         int64     `json:"redeemed_units"`
	RemainingUnits        *int64    `json:"remaining_units"`
}

func offerViews(t *testing.T, server *Server, user *storage.User, admin bool) []exchangeOfferViewTest {
	t.Helper()
	var body struct {
		Offers []exchangeOfferViewTest `json:"offers"`
	}
	if err := json.Unmarshal([]byte(listOffersBody(t, server, user, admin)), &body); err != nil {
		t.Fatal(err)
	}
	return body.Offers
}

func listOffersBody(t *testing.T, server *Server, user *storage.User, admin bool) string {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Set(authUserKey, user)
	if admin {
		server.listAdminExchangeOffers(c)
	} else {
		server.listExchangeOffers(c)
	}
	if w.Code != 200 {
		t.Fatalf("list offers status=%d body=%s", w.Code, w.Body.String())
	}
	return w.Body.String()
}
