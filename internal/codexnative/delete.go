package codexnative

import (
	"context"
	"errors"
	"fmt"
	"pocketagents/internal/core"
	"strings"
	"time"
)

func (p *Provider) deleteHistory(ctx context.Context, a core.Action) (core.Result, error) {
	res := core.Result{ActionID: a.ID, NativeID: a.NativeID, Status: "rejected"}
	held, err := p.writerHeld(a.NativeID)
	if err != nil {
		return res, err
	}
	if held {
		return res, errors.New("会话仍在运行，请先在原生客户端关闭此会话再删除；不会停止共享服务。")
	}
	c, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	b, err := p.command(c, "delete", "--force", a.NativeID).CombinedOutput()
	if err != nil {
		if c.Err() != nil {
			res.Status = "unknown"
			res.Message = "删除结果尚未确认，请核对原生历史。"
			return res, nil
		}
		return res, fmt.Errorf("native delete: %w: %s", err, strings.TrimSpace(string(b)))
	}
	res.Status = "accepted"
	res.Message = "原生 Codex 已删除此会话；项目文件保留。"
	return res, nil
}
