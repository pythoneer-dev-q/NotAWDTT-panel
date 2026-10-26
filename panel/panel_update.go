package panel

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const panelUpdateRepo = "https://github.com/pythoneer-dev-q/NotAWDTT-panel.git"

var gitCommitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

func panelReleaseBinName() string {
	switch runtime.GOARCH {
	case "arm64":
		return "wdtt-linux-arm64"
	default:
		return "wdtt-linux-amd64"
	}
}

func panelInstallPath() string {
	if p := strings.TrimSpace(os.Getenv("WDTT_PANEL_BIN")); p != "" {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		base := filepath.Base(exe)
		if base == "wdtt" || base == "wdtt-app" || base == "wdtt-panel" {
			return exe
		}
	}
	if serviceUnitExists(panelServiceUnit) {
		return "/usr/local/bin/wdtt-panel"
	}
	return wdttServerBin
}

type panelUpdateStatus struct {
	CurrentVersion  string `json:"current_version"`
	CurrentCommit   string `json:"current_commit"`
	LatestCommit    string `json:"latest_commit"`
	UpdateAvailable bool   `json:"update_available"`
	CheckedAt       int64  `json:"checked_at"`
}

func checkPanelUpdate(ctx context.Context) (panelUpdateStatus, error) {
	status := panelUpdateStatus{CurrentVersion: FormatPanelVersion(), CurrentCommit: FormatPanelBuildCommit()}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "ls-remote", panelUpdateRepo, "refs/heads/main")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		return status, fmt.Errorf("не удалось проверить GitHub: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 || !gitCommitPattern.MatchString(fields[0]) || fields[1] != "refs/heads/main" {
		return status, fmt.Errorf("GitHub вернул неверную версию ветки main")
	}
	status.LatestCommit = fields[0]
	status.UpdateAvailable = status.CurrentCommit == "" || status.CurrentCommit != status.LatestCommit
	status.CheckedAt = time.Now().Unix()
	return status, nil
}
