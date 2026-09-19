package campaign

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/gameplay"
	"github.com/ialtone/fatecraft/internal/roll"
	"github.com/ialtone/fatecraft/internal/storage"
)

type CampaignRollOpponent struct {
	RealmGap int
	Statuses []string
	Element  string
}

type CampaignRollContext struct {
	Advantages    []string
	Disadvantages []string
	Environment   []string
	Opponent      *CampaignRollOpponent
}

// CampaignRollRequest contains only server-derived inputs. Domain action
// handlers build it after their prerequisites have been checked.
type CampaignRollRequest struct {
	Key                string
	Title              string
	Description        string
	Category           string
	BaseSuccessRate    int
	Sides              int
	RewardValue        int
	RelevantAttributes []string
	AttributeWeights   map[string]float64
	PlayerState        map[string]any
	Context            *CampaignRollContext
}

type CampaignRollResult struct {
	Success     bool
	Level       string
	Rolled      int
	Sides       int
	SuccessRate int
	Breakdown   *roll.Breakdown
	Snapshot    *storage.CampaignRollSnapshot
}

type CampaignRollFunc func(context.Context, CampaignRollRequest) (*CampaignRollResult, error)

type systemSettingsProvider interface {
	Get(context.Context) (*storage.SystemSettings, error)
}

// SetRollServices enables authoritative rolls for probabilistic Campaign
// actions. Keeping this explicit preserves lightweight unit construction.
func (s *Service) SetRollServices(service *roll.Service, settings systemSettingsProvider) {
	s.roll = service
	s.systemSettings = settings
}

// campaignRollFunc 构造绑定到本次动作的判定函数。platform 非 nil 时直接使用
// 回合内固定的平台设置；否则（玩家直接发起的 HTTP 动作）在事务外读取一次。
func (s *Service) campaignRollFunc(service *roll.Service, platform *storage.SystemSettings, conversationID uuid.UUID) CampaignRollFunc {
	if s == nil || service == nil || conversationID == uuid.Nil {
		return nil
	}
	return func(ctx context.Context, request CampaignRollRequest) (*CampaignRollResult, error) {
		if platform == nil {
			platform = storage.DefaultSystemSettings()
			if s.systemSettings != nil {
				loaded, err := s.systemSettings.Get(ctx)
				if err != nil {
					return nil, err
				}
				if loaded != nil {
					platform = loaded
				}
			}
		}
		if !platform.RollSystemEnabled {
			return nil, nil
		}
		if request.Sides == 0 {
			request.Sides = 100
		}
		if strings.TrimSpace(request.Key) == "" || strings.TrimSpace(request.Title) == "" ||
			strings.TrimSpace(request.Description) == "" || strings.TrimSpace(request.Category) == "" {
			return nil, errors.New("Campaign 天命判定参数无效")
		}
		var rollContext *roll.Context
		if request.Context != nil {
			rollContext = &roll.Context{
				Advantages: request.Context.Advantages, Disadvantages: request.Context.Disadvantages,
				Environment: request.Context.Environment,
			}
			if request.Context.Opponent != nil {
				rollContext.Opponent = &roll.Opponent{
					RealmGap: request.Context.Opponent.RealmGap, Statuses: request.Context.Opponent.Statuses,
					Element: request.Context.Opponent.Element,
				}
			}
		}
		detailed := service.CalculateResult(ctx, &roll.Request{
			Key: request.Key, Title: request.Title, Description: request.Description, Category: request.Category,
			BaseSuccessRate: request.BaseSuccessRate, Sides: request.Sides, RewardValue: request.RewardValue,
			RelevantAttributes: request.RelevantAttributes, AttributeWeights: request.AttributeWeights, Context: rollContext,
		}, request.PlayerState, conversationID, platform.RollDifficultyMode)
		outcome, err := roll.ExecuteRoll(detailed, request.Sides)
		if err != nil {
			return nil, err
		}
		formatted := roll.FormatResult(request.Title, request.Description, outcome)
		metadata := storage.JSONMap{
			"roll_key": request.Key, "roll_title": request.Title, "roll_category": request.Category,
			"roll_outcome": outcome, "conditions": []any{}, "details_version": gameplay.RollDetailsVersion,
			"campaign_domain_roll": true,
		}
		return &CampaignRollResult{
			Success: outcome.Success, Level: outcome.Level, Rolled: outcome.Rolled, Sides: outcome.Sides,
			SuccessRate: outcome.SuccessRate, Breakdown: outcome.Breakdown,
			Snapshot: &storage.CampaignRollSnapshot{
				RollKey: request.Key, Title: request.Title, Category: request.Category, Success: outcome.Success,
				Content: formatted, Markdown: formatted, Metadata: metadata,
			},
		}, nil
	}
}
