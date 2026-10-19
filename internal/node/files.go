package node

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"pocketagents/internal/config"
	"pocketagents/internal/core"
	"pocketagents/internal/workspacefiles"
	"strings"
)

func (n *Node) prepareFiles(a *core.Action) error {
	if len(a.Files) == 0 {
		return nil
	}
	if a.Kind != "send" && a.Kind != "create" && a.Kind != "resume" {
		return errors.New("attachments require a send, create or resume action")
	}
	if len(a.Files) > 8 {
		return errors.New("at most eight attachments per message")
	}
	cwd, err := config.ValidatePath(n.C.Roots, a.Cwd, false)
	if err != nil {
		return err
	}
	if strings.TrimSpace(a.Text) == "" {
		a.Text = "请查看所附文件。"
	}
	for _, path := range a.Files {
		canonical, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(cwd, canonical)
		if err != nil || !filepath.IsLocal(rel) {
			return errors.New("attachment must be inside this session's workspace")
		}
		b, err := workspacefiles.Read(cwd, rel)
		if err != nil {
			return fmt.Errorf("attachment unavailable: %w", err)
		}
		mime := http.DetectContentType(b)
		a.InputFiles = append(a.InputFiles, core.FileInput{Path: filepath.Join(cwd, rel), MIME: mime})
		a.Text += fmt.Sprintf("\n附件（执行设备上的文件路径）：%q", filepath.Join(cwd, rel))
	}
	return nil
}
