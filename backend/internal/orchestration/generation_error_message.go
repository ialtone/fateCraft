package orchestration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/storage"
)

func partialGenerationErrorMessage(err error) string {
	if errors.Is(storage.GameplayPolicyError(err), storage.ErrVersionBlocked) {
		return "已提交的动作保留，但管理员已暂停此玩法版本，后续叙事暂时无法生成。解除暂停后可继续叙事，不会重复执行动作。"
	}
	// A committed action must be continued, rather than repeated. Keep the
	// diagnosis but replace recovery advice intended for uncommitted attempts.
	reason, _, _ := strings.Cut(generationErrorMessage(err), "请")
	return "动作已执行，但叙事未完成。原因：" + strings.TrimSpace(reason) + "请使用继续叙事，不会重复执行已提交的动作。"
}

// Explain known failures without exposing provider bodies, URLs or credentials.
func generationErrorMessage(err error) string {
	err = storage.GameplayPolicyError(err)
	switch {
	case errors.Is(err, storage.ErrVersionBlocked):
		return "管理员已暂停此玩法版本，暂时无法继续生成。已有游戏内容、结局和积分保留，解除暂停后可继续。"
	case errors.Is(err, llm.ErrToolCallConflict):
		return "模型服务返回的工具调用标识、名称或参数不一致，本次调用已拒绝执行。请重试或切换兼容的模型接口。"
	case errors.Is(err, context.DeadlineExceeded):
		return "生成超时，请稍后重试；若频繁超时，可切换响应更快的模型或缩短输入。"
	case errors.Is(err, llm.ErrLLMStreamStalled):
		return "模型长时间未返回数据，连接已停止。请重试或检查模型服务是否可用。"
	case errors.Is(err, llm.ErrTruncated):
		return "模型输出达到回复 Token 上限，叙事未完成。请提高最大回复 Token 或缩短回复要求后重试。"
	case errors.Is(err, llm.ErrRelayNoKey):
		return "本设备未保存 API Key，请在设置中填写后重试。"
	case errors.Is(err, llm.ErrRelayUnavailable), errors.Is(err, llm.ErrRelayLost):
		return "客户端直连未就绪或已断开，请保持页面打开，检查网络并等待连接恢复后重试。"
	case errors.Is(err, llm.ErrRelayNetwork):
		return "浏览器无法连接模型服务，请检查 API 地址、网络和服务的跨域设置。"
	case errors.Is(err, storage.ErrLLMRouteMismatch):
		return "当前模型接入方式与此对话不一致，请切回对话原接入方式，或新建对话。"
	case errors.Is(err, llm.ErrCapabilityUnsupported):
		return "当前模型不支持所需能力，请检查模型能力配置或切换支持工具调用的模型。"
	case errors.Is(err, llm.ErrToolReplayInvalidHistory):
		return "历史工具记录无法合法回放，请尝试重新生成；若仍失败，请提供关联 ID 排查。"
	case errors.Is(err, llm.ErrLLMResponseTooLarge), errors.Is(err, llm.ErrLLMSSELineTooLarge):
		return "模型返回的数据超过处理上限，请减少最大回复 Token 或切换模型后重试。"
	case errors.Is(err, errAgentToolPersistence):
		return "保存本轮工具结果失败，请稍后重试；若仍失败，请提供关联 ID 排查。"
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return "模型连接在响应完成前中断，请检查网络或模型服务后重试。"
	}
	var upstream *llm.HTTPError
	if errors.As(err, &upstream) {
		switch upstream.StatusCode {
		case 401:
			return "模型服务拒绝鉴权（HTTP 401），请检查 API Key 是否正确或已失效。"
		case 403:
			return "模型服务拒绝访问（HTTP 403），请检查账号和 API Key 是否拥有该模型的使用权限。"
		case 402:
			return "模型服务要求付费（HTTP 402），请检查服务商账户余额或套餐。"
		case 404:
			return "模型或接口不存在（HTTP 404），请检查 API 地址、接口协议和模型名称。"
		case 429:
			return "模型服务限制了请求（HTTP 429），可能是频率限制或额度不足，请稍后重试并检查服务商额度。"
		case 400, 422:
			return fmt.Sprintf("模型服务不接受本次请求（HTTP %d），请检查接口协议、模型能力和上下文长度。", upstream.StatusCode)
		default:
			if upstream.StatusCode >= 500 {
				return fmt.Sprintf("模型服务暂时异常（HTTP %d），请稍后重试或切换服务。", upstream.StatusCode)
			}
			return fmt.Sprintf("模型请求被拒绝（HTTP %d），请检查模型服务配置后重试。", upstream.StatusCode)
		}
	}
	var network net.Error
	if errors.As(err, &network) {
		if network.Timeout() {
			return "连接模型服务超时，请检查网络和 API 地址后重试。"
		}
		return "连接模型服务失败，请检查网络、API 地址和服务可用性后重试。"
	}
	return "本轮生成未完成，模型响应或执行流程发生异常。请重试；若重复发生，请提供关联 ID 排查。"
}
