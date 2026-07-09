package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"clash-jump-manager/internal/jump"
	"clash-jump-manager/internal/server"
	"clash-jump-manager/internal/store"
)

type previewPayload struct {
	Ready        bool   `json:"ready"`
	PreviewToken string `json:"preview_token"`
	HasChanges   bool   `json:"has_changes"`
	ChangeCount  int    `json:"change_count"`
}

func TestPreviewReturnsRedactedScriptWithoutWritingClashFiles(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	profilesDir := filepath.Join(configDir, "profiles")
	statePath := filepath.Join(root, "state.json")
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}
	profilesYAML := `items:
  - type: remote
    uid: source
    name: Source
  - type: remote
    uid: target
    name: Target
    option:
      script: targetScript
  - type: script
    uid: targetScript
`
	if err := os.WriteFile(filepath.Join(configDir, "profiles.yaml"), []byte(profilesYAML), 0o644); err != nil {
		t.Fatalf("write profiles.yaml: %v", err)
	}
	sourceYAML := `proxies:
  - name: jump
    type: hysteria2
    server: jump.example.com
    port: 443
    password: secret-password
    sni: jump.example.com
`
	targetYAML := `proxies:
  - name: target-node
    type: hysteria2
    server: 1.2.3.4
    port: 4001
`
	if err := os.WriteFile(filepath.Join(profilesDir, "source.yaml"), []byte(sourceYAML), 0o644); err != nil {
		t.Fatalf("write source yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "target.yaml"), []byte(targetYAML), 0o644); err != nil {
		t.Fatalf("write target yaml: %v", err)
	}
	if err := store.SaveState(statePath, jump.State{
		Enabled: true,
		Source:  &jump.JumpSource{SubUID: "source", Node: jump.JumpNode{Name: "jump", Server: "jump.example.com", Port: 443}},
		Targets: []string{"target"},
		TargetRules: map[string]jump.MatchRule{
			"target": {Mode: "server_port", ServerPorts: []jump.ServerPort{{Server: "1.2.3.4", Port: 4001}}},
		},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}
	app, err := server.New(server.Options{
		ConfigDir: configDir,
		StateFile: statePath,
		BackupDir: backupDir,
		StaticFS:  fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}},
	})
	if err != nil {
		t.Fatalf("server.New returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/jump/preview", nil)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"ready":true`) || !strings.Contains(body, `"script_redacted"`) {
		t.Fatalf("preview did not return ready redacted script: %s", body)
	}
	if !strings.Contains(body, `"has_changes":true`) {
		t.Fatalf("preview should report pending changes when target script is missing: %s", body)
	}
	if strings.Contains(body, "secret-password") {
		t.Fatalf("preview leaked source password: %s", body)
	}
	if _, err := os.Stat(filepath.Join(profilesDir, "targetScript.js")); !os.IsNotExist(err) {
		t.Fatalf("preview must not write target script, stat err=%v", err)
	}
}

func TestPreviewReportsNoChangesWhenManagedScriptAlreadyMatches(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	profilesDir := filepath.Join(configDir, "profiles")
	statePath := filepath.Join(root, "state.json")
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}
	profilesYAML := `items:
  - type: remote
    uid: source
    name: Source
  - type: remote
    uid: target
    name: Target
    option:
      script: targetScript
  - type: script
    uid: targetScript
`
	if err := os.WriteFile(filepath.Join(configDir, "profiles.yaml"), []byte(profilesYAML), 0o644); err != nil {
		t.Fatalf("write profiles.yaml: %v", err)
	}
	sourceYAML := `proxies:
  - name: jump
    type: hysteria2
    server: jump.example.com
    port: 443
`
	targetYAML := `proxies:
  - name: target-node
    type: hysteria2
    server: 1.2.3.4
    port: 4001
`
	if err := os.WriteFile(filepath.Join(profilesDir, "source.yaml"), []byte(sourceYAML), 0o644); err != nil {
		t.Fatalf("write source yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "target.yaml"), []byte(targetYAML), 0o644); err != nil {
		t.Fatalf("write target yaml: %v", err)
	}
	rule := jump.MatchRule{Mode: "server_port", ServerPorts: []jump.ServerPort{{Server: "1.2.3.4", Port: 4001}}}
	script, err := jump.GenerateScript(jump.JumpNode{Name: "jump", Type: "hysteria2", Server: "jump.example.com", Port: 443}, rule)
	if err != nil {
		t.Fatalf("generate script: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "targetScript.js"), []byte(script), 0o644); err != nil {
		t.Fatalf("write target script: %v", err)
	}
	if err := store.SaveState(statePath, jump.State{
		Enabled: true,
		Source:  &jump.JumpSource{SubUID: "source", Node: jump.JumpNode{Name: "jump", Server: "jump.example.com", Port: 443}},
		Targets: []string{"target"},
		TargetRules: map[string]jump.MatchRule{
			"target": rule,
		},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}
	app, err := server.New(server.Options{
		ConfigDir: configDir,
		StateFile: statePath,
		BackupDir: backupDir,
		StaticFS:  fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}},
	})
	if err != nil {
		t.Fatalf("server.New returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/jump/preview", nil)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", rec.Code, rec.Body.String())
	}
	var preview previewPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if !preview.Ready {
		t.Fatalf("expected ready preview: %#v body=%s", preview, rec.Body.String())
	}
	if preview.HasChanges || preview.ChangeCount != 0 || preview.PreviewToken != "" {
		t.Fatalf("expected no pending changes and no token, got %#v body=%s", preview, rec.Body.String())
	}
}

func TestPreviewReportsChangeWhenDisabledStateNeedsToClearManagedJump(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	profilesDir := filepath.Join(configDir, "profiles")
	statePath := filepath.Join(root, "state.json")
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}
	profilesYAML := `items:
  - type: remote
    uid: source
    name: Source
  - type: remote
    uid: target
    name: Target
    option:
      script: targetScript
  - type: script
    uid: targetScript
`
	if err := os.WriteFile(filepath.Join(configDir, "profiles.yaml"), []byte(profilesYAML), 0o644); err != nil {
		t.Fatalf("write profiles.yaml: %v", err)
	}
	sourceYAML := `proxies:
  - name: jump
    type: hysteria2
    server: jump.example.com
    port: 443
`
	targetYAML := `proxies:
  - name: target-node
    type: hysteria2
    server: 1.2.3.4
    port: 4001
`
	if err := os.WriteFile(filepath.Join(profilesDir, "source.yaml"), []byte(sourceYAML), 0o644); err != nil {
		t.Fatalf("write source yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "target.yaml"), []byte(targetYAML), 0o644); err != nil {
		t.Fatalf("write target yaml: %v", err)
	}
	rule := jump.MatchRule{Mode: "server_port", ServerPorts: []jump.ServerPort{{Server: "1.2.3.4", Port: 4001}}}
	script, err := jump.GenerateScript(jump.JumpNode{Name: "jump", Type: "hysteria2", Server: "jump.example.com", Port: 443}, rule)
	if err != nil {
		t.Fatalf("generate script: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "targetScript.js"), []byte(script), 0o644); err != nil {
		t.Fatalf("write target script: %v", err)
	}
	activeState := jump.State{
		Enabled: true,
		Source:  &jump.JumpSource{SubUID: "source", Node: jump.JumpNode{Name: "jump", Server: "jump.example.com", Port: 443}},
		Targets: []string{"target"},
		TargetRules: map[string]jump.MatchRule{
			"target": rule,
		},
	}
	if err := store.SaveState(statePath, activeState); err != nil {
		t.Fatalf("save state: %v", err)
	}
	app, err := server.New(server.Options{
		ConfigDir: configDir,
		StateFile: statePath,
		BackupDir: backupDir,
		StaticFS:  fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}},
	})
	if err != nil {
		t.Fatalf("server.New returned error: %v", err)
	}

	disabledDraft := activeState
	disabledDraft.Enabled = false
	body, err := json.Marshal(map[string]jump.State{"state": disabledDraft})
	if err != nil {
		t.Fatalf("marshal disabled draft: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/jump/preview", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", rec.Code, rec.Body.String())
	}
	var preview previewPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if !preview.Ready || !preview.HasChanges || preview.ChangeCount != 1 || preview.PreviewToken == "" {
		t.Fatalf("expected disabled state to require applying noop script, got %#v body=%s", preview, rec.Body.String())
	}
}

func TestPostPreviewUsesDraftStateWithoutSavingUntilApply(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	profilesDir := filepath.Join(configDir, "profiles")
	statePath := filepath.Join(root, "state.json")
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}
	profilesYAML := `items:
  - type: remote
    uid: source
    name: Source
  - type: remote
    uid: target
    name: Target
    option:
      script: targetScript
  - type: script
    uid: targetScript
`
	if err := os.WriteFile(filepath.Join(configDir, "profiles.yaml"), []byte(profilesYAML), 0o644); err != nil {
		t.Fatalf("write profiles.yaml: %v", err)
	}
	sourceYAML := `proxies:
  - name: jump
    type: hysteria2
    server: jump.example.com
    port: 443
`
	targetYAML := `proxies:
  - name: target-node
    type: hysteria2
    server: 1.2.3.4
    port: 4001
`
	if err := os.WriteFile(filepath.Join(profilesDir, "source.yaml"), []byte(sourceYAML), 0o644); err != nil {
		t.Fatalf("write source yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "target.yaml"), []byte(targetYAML), 0o644); err != nil {
		t.Fatalf("write target yaml: %v", err)
	}
	if err := store.SaveState(statePath, jump.State{
		Enabled:     false,
		Targets:     []string{},
		TargetRules: map[string]jump.MatchRule{},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}
	app, err := server.New(server.Options{
		ConfigDir: configDir,
		StateFile: statePath,
		BackupDir: backupDir,
		StaticFS:  fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}},
	})
	if err != nil {
		t.Fatalf("server.New returned error: %v", err)
	}

	draft := jump.State{
		Enabled: true,
		Source:  &jump.JumpSource{SubUID: "source", Node: jump.JumpNode{Name: "jump", Server: "jump.example.com", Port: 443}},
		Targets: []string{"target"},
		TargetRules: map[string]jump.MatchRule{
			"target": {Mode: "server_port", ServerPorts: []jump.ServerPort{{Server: "1.2.3.4", Port: 4001}}},
		},
	}
	previewBody, err := json.Marshal(map[string]jump.State{"state": draft})
	if err != nil {
		t.Fatalf("marshal preview body: %v", err)
	}
	previewReq := httptest.NewRequest(http.MethodPost, "/api/jump/preview", bytes.NewReader(previewBody))
	previewRec := httptest.NewRecorder()
	app.Handler().ServeHTTP(previewRec, previewReq)
	if previewRec.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", previewRec.Code, previewRec.Body.String())
	}
	var preview previewPayload
	if err := json.Unmarshal(previewRec.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if !preview.Ready || !preview.HasChanges || preview.PreviewToken == "" {
		t.Fatalf("expected draft preview with changes and token: %#v body=%s", preview, previewRec.Body.String())
	}
	savedBeforeApply, err := store.LoadState(statePath)
	if err != nil {
		t.Fatalf("load state before apply: %v", err)
	}
	if savedBeforeApply.Enabled || len(savedBeforeApply.Targets) != 0 {
		t.Fatalf("draft preview must not save state before apply: %#v", savedBeforeApply)
	}

	applyBody, err := json.Marshal(map[string]string{"preview_token": preview.PreviewToken})
	if err != nil {
		t.Fatalf("marshal apply body: %v", err)
	}
	applyReq := httptest.NewRequest(http.MethodPost, "/api/apply", bytes.NewReader(applyBody))
	applyRec := httptest.NewRecorder()
	app.Handler().ServeHTTP(applyRec, applyReq)
	if applyRec.Code != http.StatusOK {
		t.Fatalf("apply status=%d body=%s", applyRec.Code, applyRec.Body.String())
	}
	savedAfterApply, err := store.LoadState(statePath)
	if err != nil {
		t.Fatalf("load state after apply: %v", err)
	}
	if !savedAfterApply.Enabled || !containsString(savedAfterApply.Targets, "target") {
		t.Fatalf("apply should save the previewed draft as applied state: %#v", savedAfterApply)
	}
}

func TestJumpStateReconcilesStaleSavedTargetsFromManagedScripts(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	profilesDir := filepath.Join(configDir, "profiles")
	statePath := filepath.Join(root, "state.json")
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}
	profilesYAML := `items:
  - type: remote
    uid: source
    name: Source Sub
  - type: remote
    uid: activeTarget
    name: Active Target
    option:
      script: activeScript
  - type: remote
    uid: staleTarget
    name: Stale Target
    option:
      script: staleScript
  - type: script
    uid: activeScript
  - type: script
    uid: staleScript
`
	if err := os.WriteFile(filepath.Join(configDir, "profiles.yaml"), []byte(profilesYAML), 0o644); err != nil {
		t.Fatalf("write profiles.yaml: %v", err)
	}
	sourceYAML := `proxies:
  - name: jump node
    type: hysteria2
    server: jump.example.com
    port: 443
`
	activeTargetYAML := `proxies:
  - name: target-a
    type: hysteria2
    server: 1.2.3.4
    port: 4001
  - name: target-b
    type: hysteria2
    server: 5.6.7.8
    port: 4002
`
	staleTargetYAML := `proxies:
  - name: stale-node
    type: hysteria2
    server: 9.9.9.9
    port: 9001
`
	if err := os.WriteFile(filepath.Join(profilesDir, "source.yaml"), []byte(sourceYAML), 0o644); err != nil {
		t.Fatalf("write source yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "activeTarget.yaml"), []byte(activeTargetYAML), 0o644); err != nil {
		t.Fatalf("write active target yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "staleTarget.yaml"), []byte(staleTargetYAML), 0o644); err != nil {
		t.Fatalf("write stale target yaml: %v", err)
	}
	activeRule := jump.MatchRule{
		Mode: "server_port",
		ServerPorts: []jump.ServerPort{
			{Server: "1.2.3.4", Port: 4001},
			{Server: "5.6.7.8", Port: 4002},
		},
	}
	activeScript, err := jump.GenerateScript(jump.JumpNode{Name: "jump node", Type: "hysteria2", Server: "jump.example.com", Port: 443}, activeRule)
	if err != nil {
		t.Fatalf("generate active script: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "activeScript.js"), []byte(activeScript), 0o644); err != nil {
		t.Fatalf("write active script: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "staleScript.js"), []byte(jump.NoopScript()), 0o644); err != nil {
		t.Fatalf("write stale noop script: %v", err)
	}
	if err := store.SaveState(statePath, jump.State{
		Enabled: true,
		Source:  &jump.JumpSource{SubUID: "source", Node: jump.JumpNode{Name: "jump node", Server: "jump.example.com", Port: 443}},
		Targets: []string{"activeTarget", "staleTarget"},
		TargetRules: map[string]jump.MatchRule{
			"activeTarget": {Mode: "server", Servers: []string{"wrong.example.com"}},
			"staleTarget":  {Mode: "server", Servers: []string{"9.9.9.9"}},
		},
	}); err != nil {
		t.Fatalf("save stale state: %v", err)
	}
	app, err := server.New(server.Options{
		ConfigDir: configDir,
		StateFile: statePath,
		BackupDir: backupDir,
		StaticFS:  fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}},
	})
	if err != nil {
		t.Fatalf("server.New returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/jump/state", nil)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("jump state status=%d body=%s", rec.Code, rec.Body.String())
	}
	var applied jump.State
	if err := json.Unmarshal(rec.Body.Bytes(), &applied); err != nil {
		t.Fatalf("decode applied state: %v", err)
	}
	if !applied.Enabled {
		t.Fatalf("expected actual managed jump to enable state: %#v", applied)
	}
	if len(applied.Targets) != 1 || applied.Targets[0] != "activeTarget" {
		t.Fatalf("expected only active managed jump target, got %#v body=%s", applied.Targets, rec.Body.String())
	}
	rule := applied.TargetRules["activeTarget"]
	if rule.Mode != "server_port" || len(rule.ServerPorts) != 2 {
		t.Fatalf("expected match rule from actual script, got %#v", rule)
	}
	if applied.Source == nil || applied.Source.SubUID != "source" || applied.Source.Node.Name != "jump node" {
		t.Fatalf("expected source from actual script, got %#v", applied.Source)
	}
}

func TestReconciledSourcePrefersSubscriptionMatchingScriptSecret(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	profilesDir := filepath.Join(configDir, "profiles")
	statePath := filepath.Join(root, "state.json")
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}
	profilesYAML := `items:
  - type: remote
    uid: wrongSource
    name: Wrong Source
  - type: remote
    uid: rightSource
    name: Right Source
  - type: remote
    uid: target
    name: Target
    option:
      script: targetScript
  - type: script
    uid: targetScript
`
	if err := os.WriteFile(filepath.Join(configDir, "profiles.yaml"), []byte(profilesYAML), 0o644); err != nil {
		t.Fatalf("write profiles.yaml: %v", err)
	}
	wrongSourceYAML := `proxies:
  - name: same jump
    type: hysteria2
    server: jump.example.com
    port: 443
    password: wrong-secret
`
	rightSourceYAML := `proxies:
  - name: same jump
    type: hysteria2
    server: jump.example.com
    port: 443
    password: same-name-wrong-secret
  - name: same jump
    type: hysteria2
    server: jump.example.com
    port: 443
    password: right-secret
    up: "3000"
    down: "3000"
    skip-cert-verify: true
`
	targetYAML := `proxies:
  - name: target-node
    type: hysteria2
    server: 1.2.3.4
    port: 4001
`
	if err := os.WriteFile(filepath.Join(profilesDir, "wrongSource.yaml"), []byte(wrongSourceYAML), 0o644); err != nil {
		t.Fatalf("write wrong source yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "rightSource.yaml"), []byte(rightSourceYAML), 0o644); err != nil {
		t.Fatalf("write right source yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "target.yaml"), []byte(targetYAML), 0o644); err != nil {
		t.Fatalf("write target yaml: %v", err)
	}
	rule := jump.MatchRule{Mode: "server_port", ServerPorts: []jump.ServerPort{{Server: "1.2.3.4", Port: 4001}}}
	script, err := jump.GenerateScript(jump.JumpNode{Name: "same jump", Type: "hysteria2", Server: "jump.example.com", Port: 443, Password: "right-secret", Up: "3000", Down: "3000", SkipCertVerify: true}, rule)
	if err != nil {
		t.Fatalf("generate script: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "targetScript.js"), []byte(script), 0o644); err != nil {
		t.Fatalf("write target script: %v", err)
	}
	app, err := server.New(server.Options{
		ConfigDir: configDir,
		StateFile: statePath,
		BackupDir: backupDir,
		StaticFS:  fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}},
	})
	if err != nil {
		t.Fatalf("server.New returned error: %v", err)
	}

	stateReq := httptest.NewRequest(http.MethodGet, "/api/jump/state", nil)
	stateRec := httptest.NewRecorder()
	app.Handler().ServeHTTP(stateRec, stateReq)
	if stateRec.Code != http.StatusOK {
		t.Fatalf("jump state status=%d body=%s", stateRec.Code, stateRec.Body.String())
	}
	var applied jump.State
	if err := json.Unmarshal(stateRec.Body.Bytes(), &applied); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if applied.Source == nil || applied.Source.SubUID != "rightSource" {
		t.Fatalf("expected source UID to match script secret without exposing it, got %#v body=%s", applied.Source, stateRec.Body.String())
	}
	if applied.Source.Node.Password != "" {
		t.Fatalf("public state must not expose source password: %#v", applied.Source.Node)
	}

	previewReq := httptest.NewRequest(http.MethodGet, "/api/jump/preview", nil)
	previewRec := httptest.NewRecorder()
	app.Handler().ServeHTTP(previewRec, previewReq)
	if previewRec.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", previewRec.Code, previewRec.Body.String())
	}
	var preview previewPayload
	if err := json.Unmarshal(previewRec.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if preview.HasChanges || preview.ChangeCount != 0 {
		t.Fatalf("expected no pending changes after source reconciliation, got %#v body=%s", preview, previewRec.Body.String())
	}
}

func TestApplyBacksUpExistingScriptAndUsesRealSubscriptionSecret(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	profilesDir := filepath.Join(configDir, "profiles")
	statePath := filepath.Join(root, "state.json")
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}
	profilesYAML := `items:
  - type: remote
    uid: source
    name: Source
  - type: remote
    uid: target
    name: Target
    option:
      script: targetScript
  - type: script
    uid: targetScript
`
	if err := os.WriteFile(filepath.Join(configDir, "profiles.yaml"), []byte(profilesYAML), 0o644); err != nil {
		t.Fatalf("write profiles.yaml: %v", err)
	}
	sourceYAML := `proxies:
  - name: jump
    type: hysteria2
    server: jump.example.com
    port: 443
    password: real-secret-password
    sni: jump.example.com
`
	targetYAML := `proxies:
  - name: target-node
    type: hysteria2
    server: 1.2.3.4
    port: 4001
`
	if err := os.WriteFile(filepath.Join(profilesDir, "source.yaml"), []byte(sourceYAML), 0o644); err != nil {
		t.Fatalf("write source yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "target.yaml"), []byte(targetYAML), 0o644); err != nil {
		t.Fatalf("write target yaml: %v", err)
	}
	existingScript := "// user script before apply\nfunction main(config) { return config; }\n"
	scriptPath := filepath.Join(profilesDir, "targetScript.js")
	if err := os.WriteFile(scriptPath, []byte(existingScript), 0o644); err != nil {
		t.Fatalf("write existing target script: %v", err)
	}
	if err := store.SaveState(statePath, jump.State{
		Enabled: true,
		Source:  &jump.JumpSource{SubUID: "source", Node: jump.JumpNode{Name: "jump", Server: "jump.example.com", Port: 443, Password: ""}},
		Targets: []string{"target"},
		TargetRules: map[string]jump.MatchRule{
			"target": {Mode: "server_port", ServerPorts: []jump.ServerPort{{Server: "1.2.3.4", Port: 4001}}},
		},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}
	app, err := server.New(server.Options{
		ConfigDir: configDir,
		StateFile: statePath,
		BackupDir: backupDir,
		StaticFS:  fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}},
	})
	if err != nil {
		t.Fatalf("server.New returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/apply", nil)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("apply without preview token status=%d body=%s", rec.Code, rec.Body.String())
	}

	previewReq := httptest.NewRequest(http.MethodGet, "/api/jump/preview", nil)
	previewRec := httptest.NewRecorder()
	app.Handler().ServeHTTP(previewRec, previewReq)
	if previewRec.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", previewRec.Code, previewRec.Body.String())
	}
	var preview previewPayload
	if err := json.Unmarshal(previewRec.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if !preview.Ready || preview.PreviewToken == "" {
		t.Fatalf("expected ready preview with token: %#v body=%s", preview, previewRec.Body.String())
	}

	applyBody, err := json.Marshal(map[string]string{"preview_token": preview.PreviewToken})
	if err != nil {
		t.Fatalf("marshal apply body: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/apply", bytes.NewReader(applyBody))
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("apply status=%d body=%s", rec.Code, rec.Body.String())
	}

	written, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read written script: %v", err)
	}
	if !strings.Contains(string(written), jump.ManagedMarker) {
		t.Fatalf("expected managed marker in written script:\n%s", written)
	}
	if !strings.Contains(string(written), "real-secret-password") {
		t.Fatalf("expected apply to load real secret from subscription cache:\n%s", written)
	}
	backups, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one backup file, got %d", len(backups))
	}
	backup, err := os.ReadFile(filepath.Join(backupDir, backups[0].Name()))
	if err != nil {
		t.Fatalf("read backup file: %v", err)
	}
	if string(backup) != existingScript {
		t.Fatalf("backup did not preserve existing script:\n%s", backup)
	}
}

func TestStateChangeInvalidatesPreviewToken(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	profilesDir := filepath.Join(configDir, "profiles")
	statePath := filepath.Join(root, "state.json")
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}
	profilesYAML := `items:
  - type: remote
    uid: source
    name: Source
  - type: remote
    uid: target
    name: Target
    option:
      script: targetScript
  - type: script
    uid: targetScript
`
	if err := os.WriteFile(filepath.Join(configDir, "profiles.yaml"), []byte(profilesYAML), 0o644); err != nil {
		t.Fatalf("write profiles.yaml: %v", err)
	}
	sourceYAML := `proxies:
  - name: jump
    type: hysteria2
    server: jump.example.com
    port: 443
    password: real-secret-password
`
	targetYAML := `proxies:
  - name: target-node
    type: hysteria2
    server: 1.2.3.4
    port: 4001
`
	if err := os.WriteFile(filepath.Join(profilesDir, "source.yaml"), []byte(sourceYAML), 0o644); err != nil {
		t.Fatalf("write source yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "target.yaml"), []byte(targetYAML), 0o644); err != nil {
		t.Fatalf("write target yaml: %v", err)
	}
	if err := store.SaveState(statePath, jump.State{
		Enabled: true,
		Source:  &jump.JumpSource{SubUID: "source", Node: jump.JumpNode{Name: "jump", Server: "jump.example.com", Port: 443}},
		Targets: []string{"target"},
		TargetRules: map[string]jump.MatchRule{
			"target": {Mode: "server_port", ServerPorts: []jump.ServerPort{{Server: "1.2.3.4", Port: 4001}}},
		},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}
	app, err := server.New(server.Options{
		ConfigDir: configDir,
		StateFile: statePath,
		BackupDir: backupDir,
		StaticFS:  fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}},
	})
	if err != nil {
		t.Fatalf("server.New returned error: %v", err)
	}

	previewReq := httptest.NewRequest(http.MethodGet, "/api/jump/preview", nil)
	previewRec := httptest.NewRecorder()
	app.Handler().ServeHTTP(previewRec, previewReq)
	if previewRec.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", previewRec.Code, previewRec.Body.String())
	}
	var preview previewPayload
	if err := json.Unmarshal(previewRec.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if preview.PreviewToken == "" {
		t.Fatalf("expected preview token: %s", previewRec.Body.String())
	}

	toggleReq := httptest.NewRequest(http.MethodPut, "/api/jump/toggle", strings.NewReader(`{"enabled":false}`))
	toggleRec := httptest.NewRecorder()
	app.Handler().ServeHTTP(toggleRec, toggleReq)
	if toggleRec.Code != http.StatusOK {
		t.Fatalf("toggle status=%d body=%s", toggleRec.Code, toggleRec.Body.String())
	}

	applyBody, err := json.Marshal(map[string]string{"preview_token": preview.PreviewToken})
	if err != nil {
		t.Fatalf("marshal apply body: %v", err)
	}
	applyReq := httptest.NewRequest(http.MethodPost, "/api/apply", bytes.NewReader(applyBody))
	applyRec := httptest.NewRecorder()
	app.Handler().ServeHTTP(applyRec, applyReq)
	if applyRec.Code != http.StatusConflict {
		t.Fatalf("expected stale preview token to be rejected, status=%d body=%s", applyRec.Code, applyRec.Body.String())
	}
}

func TestRuntimeStatusDetectsCurrentManagedJumpFromActiveProfile(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	profilesDir := filepath.Join(configDir, "profiles")
	statePath := filepath.Join(root, "state.json")
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}
	profilesYAML := `current: target
items:
  - type: remote
    uid: source
    name: Source Sub
  - type: remote
    uid: target
    name: Target Sub
    option:
      script: targetScript
  - type: script
    uid: targetScript
`
	if err := os.WriteFile(filepath.Join(configDir, "profiles.yaml"), []byte(profilesYAML), 0o644); err != nil {
		t.Fatalf("write profiles.yaml: %v", err)
	}
	sourceYAML := `proxies:
  - name: jump node
    type: hysteria2
    server: jump.example.com
    port: 443
`
	targetYAML := `proxies:
  - name: target-node
    type: hysteria2
    server: 1.2.3.4
    port: 4001
`
	if err := os.WriteFile(filepath.Join(profilesDir, "source.yaml"), []byte(sourceYAML), 0o644); err != nil {
		t.Fatalf("write source yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "target.yaml"), []byte(targetYAML), 0o644); err != nil {
		t.Fatalf("write target yaml: %v", err)
	}
	script, err := jump.GenerateScript(jump.JumpNode{Name: "jump node", Type: "hysteria2", Server: "jump.example.com", Port: 443}, jump.MatchRule{
		Mode:        "server_port",
		ServerPorts: []jump.ServerPort{{Server: "1.2.3.4", Port: 4001}},
	})
	if err != nil {
		t.Fatalf("generate script: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "targetScript.js"), []byte(script), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	app, err := server.New(server.Options{
		ConfigDir: configDir,
		StateFile: statePath,
		BackupDir: backupDir,
		StaticFS:  fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}},
	})
	if err != nil {
		t.Fatalf("server.New returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/runtime/status", nil)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, required := range []string{
		`"active":true`,
		`"script_kind":"managed_jump"`,
		`"current_uid":"target"`,
		`"target_name":"Target Sub"`,
		`"source_name":"jump node"`,
		`"source_sub_uid":"source"`,
		`"source_sub_name":"Source Sub"`,
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("expected runtime status to contain %q, body=%s", required, body)
		}
	}
}

func TestRuntimeStatusDetectsForeignJumpAndDisableRequiresConfirmation(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	profilesDir := filepath.Join(configDir, "profiles")
	statePath := filepath.Join(root, "state.json")
	backupDir := filepath.Join(root, "backups")
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}
	profilesYAML := `current: target
items:
  - type: remote
    uid: target
    name: Target Sub
    option:
      script: foreignScript
  - type: script
    uid: foreignScript
`
	if err := os.WriteFile(filepath.Join(configDir, "profiles.yaml"), []byte(profilesYAML), 0o644); err != nil {
		t.Fatalf("write profiles.yaml: %v", err)
	}
	foreignScript := "function main(config) { config.proxies.forEach(p => p['dialer-proxy'] = 'old-jump'); return config; }\n"
	scriptPath := filepath.Join(profilesDir, "foreignScript.js")
	if err := os.WriteFile(scriptPath, []byte(foreignScript), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	app, err := server.New(server.Options{
		ConfigDir: configDir,
		StateFile: statePath,
		BackupDir: backupDir,
		StaticFS:  fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}},
	})
	if err != nil {
		t.Fatalf("server.New returned error: %v", err)
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/api/runtime/status", nil)
	statusRec := httptest.NewRecorder()
	app.Handler().ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", statusRec.Code, statusRec.Body.String())
	}
	if !strings.Contains(statusRec.Body.String(), `"script_kind":"foreign_jump"`) || !strings.Contains(statusRec.Body.String(), `"can_disable_foreign":true`) {
		t.Fatalf("expected foreign jump detection, body=%s", statusRec.Body.String())
	}

	withoutConfirm := httptest.NewRequest(http.MethodPost, "/api/scripts/foreignScript/disable-foreign-jump", strings.NewReader(`{}`))
	withoutConfirmRec := httptest.NewRecorder()
	app.Handler().ServeHTTP(withoutConfirmRec, withoutConfirm)
	if withoutConfirmRec.Code != http.StatusConflict {
		t.Fatalf("expected disable without confirmation to be rejected, status=%d body=%s", withoutConfirmRec.Code, withoutConfirmRec.Body.String())
	}

	disableReq := httptest.NewRequest(http.MethodPost, "/api/scripts/foreignScript/disable-foreign-jump", strings.NewReader(`{"confirm":true}`))
	disableRec := httptest.NewRecorder()
	app.Handler().ServeHTTP(disableRec, disableReq)
	if disableRec.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", disableRec.Code, disableRec.Body.String())
	}
	after, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read disabled script: %v", err)
	}
	if strings.Contains(string(after), "dialer-proxy") {
		t.Fatalf("expected foreign jump script to be disabled:\n%s", after)
	}
	backups, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one backup, got %d", len(backups))
	}
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
