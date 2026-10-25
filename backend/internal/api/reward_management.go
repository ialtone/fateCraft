package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/storage"
)

func rewardFail(c *gin.Context, err error) {
	status, code := http.StatusBadRequest, "invalid_reward_request"
	var duplicate *storage.ExchangeDuplicateError
	switch {
	case errors.Is(err, storage.ErrExchangeChanged):
		status, code = http.StatusConflict, "exchange_revision_conflict"
	case errors.Is(err, storage.ErrExchangeUnavailable):
		code = "exchange_offer_disabled"
	case errors.Is(err, storage.ErrExchangeStock):
		status, code = http.StatusConflict, "exchange_stock_insufficient"
	case errors.As(err, &duplicate):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "exchange_cdk_duplicate", "duplicates": duplicate.Codes})
		return
	case errors.Is(err, storage.ErrTicketsDisabled):
		status, code = http.StatusForbidden, "exchange_tickets_disabled"
	case errors.Is(err, storage.ErrTicketState), errors.Is(err, storage.ErrTicketKind):
		status, code = http.StatusConflict, "exchange_ticket_state"
	case errors.Is(err, storage.ErrPointsCDKUnknown):
		status, code = http.StatusNotFound, "points_cdk_unknown"
	case errors.Is(err, storage.ErrPointsCDKUsed):
		status, code = http.StatusConflict, "points_cdk_used"
	case errors.Is(err, storage.ErrPointsCDKExpired):
		status, code = http.StatusConflict, "points_cdk_expired"
	case errors.Is(err, storage.ErrPointsCDKSameBatch):
		status, code = http.StatusConflict, "points_cdk_batch_redeemed"
	case errors.Is(err, storage.ErrExchangeLimitReached):
		status, code = http.StatusConflict, "exchange_limit_reached"
	case errors.Is(err, storage.ErrRewardRequestConflict):
		status, code = http.StatusConflict, "request_conflict"
	case errors.Is(err, storage.ErrRewardState):
		status, code = http.StatusConflict, "reward_state_conflict"
	case errors.Is(err, storage.ErrRewardAdminRequired):
		status, code = http.StatusForbidden, "admin_required"
	case errors.Is(err, storage.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, storage.ErrTargetUserDisabled):
		code = "user_disabled"
	case errors.Is(err, storage.ErrRewardKey):
		code = "invalid_reward_key"
	case errors.Is(err, storage.ErrRewardQuantity):
		code = "invalid_quantity"
	case errors.Is(err, storage.ErrRewardReversal):
		code = "reversal_unsupported"
	case errors.Is(err, storage.ErrInsufficientReward):
		code = "insufficient_balance"
	default:
		fail(c, err)
		return
	}
	c.JSON(status, gin.H{"error": err.Error(), "code": code})
}

func rewardActor(c *gin.Context) uuid.UUID {
	value, _ := c.Get(authUserKey)
	if user, ok := value.(*storage.User); ok && user != nil {
		return user.ID
	}
	return uuid.Nil
}

func rewardCursor(c *gin.Context) (storage.RewardPageCursor, bool) {
	var result storage.RewardPageCursor
	if c.Query("reward_definition_id") != "" {
		c.JSON(400, gin.H{"error": "请使用 reward_key 筛选"})
		return result, false
	}
	result.RewardKey = c.Query("reward_key")
	result.SourceType = c.Query("source_type")
	for key, dest := range map[string]*string{"pack_id": &result.PackID, "version_id": &result.VersionID} {
		if raw := c.Query(key); raw != "" {
			if _, err := uuid.Parse(raw); err != nil {
				c.JSON(400, gin.H{"error": "玩法包或版本 ID 无效"})
				return result, false
			}
			*dest = raw
		}
	}
	if result.RewardKey != "" && !storage.ValidRewardKey(result.RewardKey) {
		c.JSON(400, gin.H{"error": "余额项目无效"})
		return result, false
	}
	for name, dest := range map[string]**time.Time{"created_from": &result.CreatedFrom, "created_to": &result.CreatedTo} {
		if raw := c.Query(name); raw != "" {
			v, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				c.JSON(400, gin.H{"error": "时间筛选无效"})
				return result, false
			}
			*dest = &v
		}
	}
	if result.CreatedFrom != nil && result.CreatedTo != nil && result.CreatedFrom.After(*result.CreatedTo) {
		c.JSON(400, gin.H{"error": "开始时间不能晚于结束时间"})
		return result, false
	}
	if raw := c.Query("cursor"); raw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		parts := strings.Split(string(decoded), "|")
		if err != nil || len(parts) != 2 {
			c.JSON(400, gin.H{"error": "分页游标无效"})
			return result, false
		}
		result.Time, err = time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			c.JSON(400, gin.H{"error": "分页游标无效"})
			return result, false
		}
		result.ID, err = uuid.Parse(parts[1])
		if err != nil {
			c.JSON(400, gin.H{"error": "分页游标无效"})
			return result, false
		}
	}
	return result, true
}

func encodeRewardCursor(at time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(at.Format(time.RFC3339Nano) + "|" + id.String()))
}

func (s *Server) getAdminUserRewards(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	users, err := s.auth.ListUsers(c.Request.Context(), 1, 0, id.String())
	if err != nil {
		rewardFail(c, err)
		return
	}
	if len(users) == 0 {
		rewardFail(c, storage.ErrNotFound)
		return
	}
	items, err := s.rewards.ListPlayerRewards(c.Request.Context(), id)
	if err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, gin.H{"user": users[0], "rewards": items})
}

func (s *Server) createAdminRewardGrant(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body storage.AdminRewardGrant
	if err := bindRewardJSON(c, &body); err != nil {
		c.JSON(400, gin.H{"error": "发放请求格式无效", "code": "invalid_reward_request"})
		return
	}
	row, err := s.rewards.AdminGrant(c.Request.Context(), rewardActor(c), id, body)
	if err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, row.AdminView())
}

func (s *Server) getAdminRewardRequest(c *gin.Context) {
	id, ok := paramUUID(c, "request_id")
	if !ok {
		return
	}
	row, err := s.rewards.GrantByRequest(c.Request.Context(), id)
	if err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, row.AdminView())
}

func (s *Server) getRegistrationRewardRule(c *gin.Context) {
	rule, err := s.rewards.RegistrationRule(c.Request.Context())
	if err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, rule)
}
func (s *Server) saveRegistrationRewardRule(c *gin.Context) {
	var body struct {
		ID             int64 `json:"id"`
		Points         int64 `json:"points"`
		RewriteCredits int64 `json:"rewrite_credits"`
	}
	if err := bindRewardJSON(c, &body); err != nil {
		c.JSON(400, gin.H{"error": "注册赠送规则无效"})
		return
	}
	rule, err := s.rewards.SaveRegistrationRule(c.Request.Context(), rewardActor(c), storage.RegistrationRewardRule{ID: body.ID, Points: body.Points, RewriteCredits: body.RewriteCredits})
	if err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, rule)
}

func (s *Server) listAdminRewardLedger(c *gin.Context) { s.rewardLedgerPage(c, true) }
func (s *Server) rewardLedgerPage(c *gin.Context, admin bool) {
	limit, ok := adminListLimit(c)
	if !ok {
		return
	}
	var player *uuid.UUID
	if !admin {
		id := rewardActor(c)
		player = &id
	} else {
		raw := c.Param("id")
		if raw == "" {
			raw = c.Query("user_id")
		}
		if raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				c.JSON(400, gin.H{"error": "用户 ID 无效"})
				return
			}
			player = &id
		}
	}
	cursor, ok := rewardCursor(c)
	if !ok {
		return
	}
	rows, err := s.rewards.ListLedger(c.Request.Context(), player, limit+1, cursor)
	if err != nil {
		rewardFail(c, err)
		return
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = encodeRewardCursor(last.CreatedAt, last.ID)
	}
	for i := range rows {
		rows[i].PublicMeta = publicLedgerMeta(rows[i].Metadata)
	}
	if admin {
		views := make([]storage.AdminRewardLedgerView, 0, len(rows))
		for _, row := range rows {
			views = append(views, row.AdminView())
		}
		c.JSON(200, gin.H{"entries": views, "next_cursor": next})
		return
	}
	c.JSON(200, gin.H{"entries": rows, "next_cursor": next})
}

// publicLedgerMeta 提取账本元数据中可对玩家展示的白名单字段（目前仅兑换商品摘要）。
func publicLedgerMeta(meta storage.JSONMap) storage.JSONMap {
	out := storage.JSONMap{}
	for _, key := range []string{"offer_name", "units"} {
		if v, ok := meta[key]; ok {
			out[key] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// 奖励写接口拒绝未知字段，防止旧参数被当成新的默认配置。
func bindRewardJSON(c *gin.Context, dest any) error {
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(dest); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return storage.ErrRewardState
	}
	return nil
}
func (s *Server) getAdminRewardLedgerEntry(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	row, err := s.rewards.LedgerEntry(c.Request.Context(), id)
	if err != nil {
		rewardFail(c, err)
		return
	}
	row.PublicMeta = publicLedgerMeta(row.Metadata)
	c.JSON(200, row.AdminView())
}
func (s *Server) reverseAdminRewardGrant(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body struct {
		RequestID uuid.UUID `json:"request_id"`
		Reason    string    `json:"reason"`
	}
	if err := bindRewardJSON(c, &body); err != nil {
		c.JSON(400, gin.H{"error": "撤回请求格式无效"})
		return
	}
	row, err := s.rewards.ReverseAdminGrant(c.Request.Context(), rewardActor(c), id, body.RequestID, body.Reason)
	if err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, row.AdminView())
}
