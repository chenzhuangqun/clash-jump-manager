package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"clash-jump-manager/internal/clash"
	"clash-jump-manager/internal/jump"
)

type Action struct {
	Action     string         `json:"action,omitempty"`
	TargetUID  string         `json:"target_uid,omitempty"`
	ScriptUID  string         `json:"script_uid"`
	Path       string         `json:"path"`
	BackupPath string         `json:"backup_path,omitempty"`
	MatchRule  jump.MatchRule `json:"match_rule,omitempty"`
}

func LoadState(path string) (jump.State, error) {
	state := jump.State{Targets: []string{}, TargetRules: map[string]jump.MatchRule{}}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return state, err
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return jump.State{}, err
	}
	if state.Targets == nil {
		state.Targets = []string{}
	}
	if state.TargetRules == nil {
		state.TargetRules = map[string]jump.MatchRule{}
	}
	return state, nil
}

func SaveState(path string, state jump.State) error {
	publicState := jump.PublicState(state)
	if publicState.Targets == nil {
		publicState.Targets = []string{}
	}
	if publicState.TargetRules == nil {
		publicState.TargetRules = map[string]jump.MatchRule{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(publicState, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
}

func BackupScript(path string, backupDir string, reason string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", err
	}
	stamp := time.Now().Format("20060102-150405-000000")
	safeReason := sanitizeReason(reason)
	if safeReason == "" {
		safeReason = "write"
	}
	ext := filepath.Ext(path)
	base := filepath.Base(path)
	stem := base[:len(base)-len(ext)]
	backupPath := filepath.Join(backupDir, fmt.Sprintf("%s-%s-%s%s", stem, safeReason, stamp, ext))
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(backupPath, raw, 0o644); err != nil {
		return "", err
	}
	return backupPath, nil
}

func ResetManagedScripts(items []clash.ProfileItem, profilesDir string, backupDir string) ([]Action, error) {
	actions := []Action{}
	for _, item := range items {
		if item.Type != "script" {
			continue
		}
		jsPath := filepath.Join(profilesDir, item.UID+".js")
		raw, err := os.ReadFile(jsPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if !jump.IsManagedScript(string(raw)) {
			continue
		}
		backupPath, err := BackupScript(jsPath, backupDir, "reset")
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(jsPath, []byte(jump.NoopScript()), 0o644); err != nil {
			return nil, err
		}
		actions = append(actions, Action{
			Action:     "reset",
			ScriptUID:  item.UID,
			Path:       jsPath,
			BackupPath: backupPath,
		})
	}
	return actions, nil
}

func DisableForeignJumpScript(path string, backupDir string) (Action, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Action{}, err
	}
	inspection := jump.InspectScript(string(raw))
	if inspection.Kind != jump.ScriptKindForeignJump {
		return Action{}, fmt.Errorf("脚本不是第三方跳板脚本，已拒绝停用")
	}
	backupPath, err := BackupScript(path, backupDir, "disable-foreign-jump")
	if err != nil {
		return Action{}, err
	}
	if err := os.WriteFile(path, []byte(jump.PlainNoopScript()), 0o644); err != nil {
		return Action{}, err
	}
	scriptUID := filepath.Base(path)
	if ext := filepath.Ext(scriptUID); ext != "" {
		scriptUID = scriptUID[:len(scriptUID)-len(ext)]
	}
	return Action{
		Action:     "disabled_foreign_jump",
		ScriptUID:  scriptUID,
		Path:       path,
		BackupPath: backupPath,
	}, nil
}

func WriteScriptWithBackup(path string, backupDir string, reason string, content string) (string, error) {
	backupPath, err := BackupScript(path, backupDir, reason)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return backupPath, nil
}

var unsafeReasonRE = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func sanitizeReason(reason string) string {
	return unsafeReasonRE.ReplaceAllString(reason, "-")
}
