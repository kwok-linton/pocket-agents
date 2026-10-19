package codexnative

import (
	"context"
	"encoding/json"
	"errors"
	"pocketagents/internal/core"
	"strings"
)

type modelOption struct {
	Model                     string `json:"model"`
	DisplayName               string `json:"displayName"`
	DefaultReasoningEffort    string `json:"defaultReasoningEffort"`
	SupportedReasoningEfforts []struct {
		ReasoningEffort string `json:"reasoningEffort"`
	} `json:"supportedReasoningEfforts"`
}

func (p *Provider) catalog(ctx context.Context, r *rpcClient) ([]modelOption, error) {
	var all []modelOption
	cursor := ""
	for {
		var page struct {
			Data []modelOption `json:"data"`
			Next string        `json:"nextCursor"`
		}
		params := map[string]any{"limit": 100}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := r.call(ctx, "model/list", params, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		if page.Next == "" {
			return all, nil
		}
		cursor = page.Next
	}
}
func (p *Provider) modelOptions(ctx context.Context, r *rpcClient, a core.Action) (core.Result, error) {
	res := core.Result{ActionID: a.ID, Status: "rejected"}
	list, err := p.catalog(ctx, r)
	if err != nil {
		return res, err
	}
	res.Status = "accepted"
	res.Data, _ = json.Marshal(map[string]any{"models": list})
	return res, nil
}
func (p *Provider) validateModel(ctx context.Context, r *rpcClient, a core.Action) error {
	if a.Kind != "send" && a.Kind != "resume" && a.Kind != "create" {
		return errors.New("model override requires a message or resume")
	}
	if strings.HasPrefix(a.ExecutionID, "codex-queue:") {
		return errors.New("此会话由电脑客户端持有，原生队列不支持模型切换；请在电脑端切换")
	}
	if strings.TrimSpace(a.Text) == "" && len(a.Files) == 0 {
		return errors.New("模型与 effort 随下一条消息应用，请同时发送消息")
	}
	if a.Model == "" {
		return errors.New("选择 effort 前请先选择模型")
	}
	list, err := p.catalog(ctx, r)
	if err != nil {
		return err
	}
	for _, m := range list {
		if m.Model == a.Model {
			if a.Effort == "" {
				return nil
			}
			for _, e := range m.SupportedReasoningEfforts {
				if e.ReasoningEffort == a.Effort {
					return nil
				}
			}
			return errors.New("该模型不支持所选 effort")
		}
	}
	return errors.New("当前原生模型列表不包含此模型，请刷新")
}
