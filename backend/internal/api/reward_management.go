package api

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/storage"
)

func rewardFail(c *gin.Context, err error) {
	status, code := http.StatusBadRequest, "invalid_reward_request"
	switch {
	case errors.Is(err, storage.ErrExchangeChanged):
		status, code = http.StatusConflict, "exchange_changed"
	case errors.Is(err, storage.ErrExchangeOwned):
		status, code = http.StatusConflict, "exchange_owned"
	case errors.Is(err, storage.ErrExchangeUnavailable):
		code = "exchange_unavailable"
	case errors.Is(err, storage.ErrRewardRequestConflict):
		status, code = http.StatusConflict, "request_conflict"
	case errors.Is(err, storage.ErrRewardState):
		status, code = http.StatusConflict, "reward_state_conflict"
	case errors.Is(err, storage.ErrRewardAdminRequired):
		status, code = http.StatusForbidden, "admin_required"
	case errors.Is(err, storage.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, storage.ErrRewardDisabled):
		code = "asset_disabled"
	case errors.Is(err, storage.ErrTargetUserDisabled):
		code = "user_disabled"
	case errors.Is(err, storage.ErrRewardQuantity):
		code = "invalid_quantity"
	case errors.Is(err, storage.ErrRewardReversal):
		code = "reversal_unsupported"
	case errors.Is(err, storage.ErrInsufficientReward):
		code = "insufficient_balance"
	case errors.Is(err, storage.ErrRewardDefinitionConflict), errors.Is(err, storage.ErrRewardDefinitionInUse):
		status, code = http.StatusConflict, "asset_conflict"
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
	if value := c.Query("reward_definition_id"); value != "" {
		id, err := uuid.Parse(value)
		if err != nil {
			c.JSON(400, gin.H{"error": "奖励 ID 无效"})
			return result, false
		}
		result.DefinitionID = &id
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

func (s *Server) rewardTarget(c *gin.Context, admin bool) (uuid.UUID, bool) {
	if admin {
		return paramUUID(c, "id")
	}
	id := rewardActor(c)
	if id == uuid.Nil {
		c.JSON(401, gin.H{"error": "未登录"})
		return id, false
	}
	return id, true
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
	if err := c.ShouldBindJSON(&body); err != nil {
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

func (s *Server) listRewardRecords(c *gin.Context)      { s.rewardRecords(c, false) }
func (s *Server) listAdminRewardRecords(c *gin.Context) { s.rewardRecords(c, true) }
func (s *Server) rewardRecords(c *gin.Context, admin bool) {
	limit, ok := adminListLimit(c)
	if !ok {
		return
	}
	cursor, ok := rewardCursor(c)
	if !ok {
		return
	}
	f := storage.RewardRecordFilter{Limit: limit + 1, DefinitionID: cursor.DefinitionID, Status: c.Query("status"), SourceType: c.Query("source_type"), SourceID: c.Query("source_id"), Purpose: c.Query("purpose")}
	for name, dest := range map[string]**time.Time{"created_from": &f.CreatedFrom, "created_to": &f.CreatedTo} {
		if raw := c.Query(name); raw != "" {
			parsed, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				c.JSON(400, gin.H{"error": "时间筛选格式无效"})
				return
			}
			*dest = &parsed
		}
	}
	if f.CreatedFrom != nil && f.CreatedTo != nil && f.CreatedFrom.After(*f.CreatedTo) {
		c.JSON(400, gin.H{"error": "开始时间不能晚于结束时间"})
		return
	}
	if f.Status != "" && f.Status != "pending" && f.Status != "granted" && f.Status != "skipped" && f.Status != "cancelled" && f.Status != "reversed" {
		c.JSON(400, gin.H{"error": "状态无效"})
		return
	}
	if !cursor.Time.IsZero() {
		f.BeforeTime = &cursor.Time
		f.BeforeID = cursor.ID
	}
	if !admin {
		id := rewardActor(c)
		f.PlayerID = &id
	} else if c.Param("id") != "" {
		id, ok := paramUUID(c, "id")
		if !ok {
			return
		}
		f.PlayerID = &id
	} else if raw := c.Query("user_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(400, gin.H{"error": "用户 ID 无效"})
			return
		}
		f.PlayerID = &id
	}
	if admin && c.Query("actor_id") != "" {
		id, err := uuid.Parse(c.Query("actor_id"))
		if err != nil {
			c.JSON(400, gin.H{"error": "操作者 ID 无效"})
			return
		}
		f.ActorID = &id
	}
	rows, err := s.rewards.ListRecords(c.Request.Context(), f)
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
	if admin {
		views := make([]storage.AdminRewardRecord, 0, len(rows))
		for _, row := range rows {
			views = append(views, row.AdminView())
		}
		c.JSON(200, gin.H{"records": views, "next_cursor": next})
		return
	}
	c.JSON(200, gin.H{"records": rows, "next_cursor": next})
}

func (s *Server) getRewardRecord(c *gin.Context)      { s.rewardRecord(c, false) }
func (s *Server) getAdminRewardRecord(c *gin.Context) { s.rewardRecord(c, true) }
func (s *Server) rewardRecord(c *gin.Context, admin bool) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var player *uuid.UUID
	if !admin {
		actor := rewardActor(c)
		player = &actor
	}
	row, err := s.rewards.Record(c.Request.Context(), player, id)
	if err != nil {
		rewardFail(c, err)
		return
	}
	if admin {
		audits, err := s.rewards.RecordAudits(c.Request.Context(), id)
		if err != nil {
			rewardFail(c, err)
			return
		}
		c.JSON(200, gin.H{"record": row.AdminView(), "audits": audits})
		return
	}
	c.JSON(200, row)
}

func (s *Server) claimReward(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	row, err := s.rewards.ClaimReward(c.Request.Context(), rewardActor(c), id)
	if err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, row)
}

func (s *Server) manageReward(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body struct {
		RequestID uuid.UUID `json:"request_id"`
		Reason    string    `json:"reason"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "操作请求无效"})
		return
	}
	row, err := s.rewards.ManageReward(c.Request.Context(), rewardActor(c), id, c.Param("action"), body.Reason, body.RequestID)
	if err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, row.AdminView())
}

func (s *Server) getRewardImpact(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	impact, err := s.rewards.DefinitionImpact(c.Request.Context(), id)
	if err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, impact)
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
	var body storage.RegistrationRewardRule
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "注册赠送规则无效"})
		return
	}
	rule, err := s.rewards.SaveRegistrationRule(c.Request.Context(), rewardActor(c), body)
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
	id, ok := s.rewardTarget(c, admin)
	if !ok {
		return
	}
	cursor, ok := rewardCursor(c)
	if !ok {
		return
	}
	rows, err := s.rewards.ListLedger(c.Request.Context(), id, limit+1, cursor)
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
	c.JSON(200, gin.H{"entries": rows, "next_cursor": next})
}
