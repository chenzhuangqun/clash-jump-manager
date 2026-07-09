package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"clash-jump-manager/internal/clash"
	"clash-jump-manager/internal/jump"
	"clash-jump-manager/internal/store"
)

type Options struct {
	ConfigDir string
	StateFile string
	BackupDir string
	StaticFS  fs.FS
}

type App struct {
	client       clash.Client
	stateFile    string
	backupDir    string
	staticFS     fs.FS
	state        jump.State
	mux          *http.ServeMux
	previewToken string
	previewState *jump.State
}

type setSourceRequest struct {
	SubUID   string `json:"sub_uid"`
	NodeName string `json:"node_name"`
}

type toggleRequest struct {
	Enabled bool `json:"enabled"`
}

type setTargetRuleRequest struct {
	Rule jump.MatchRule `json:"rule"`
}

type applyRequest struct {
	PreviewToken string `json:"preview_token"`
}

type previewRequest struct {
	State *jump.State `json:"state"`
}

type disableForeignJumpRequest struct {
	Confirm bool `json:"confirm"`
}

type previewResponse struct {
	Ready        bool          `json:"ready"`
	Reason       string        `json:"reason"`
	Enabled      bool          `json:"enabled"`
	HasChanges   bool          `json:"has_changes"`
	ChangeCount  int           `json:"change_count"`
	PreviewToken string        `json:"preview_token,omitempty"`
	Items        []previewItem `json:"items"`
	State        jump.State    `json:"state"`
}

type previewItem struct {
	TargetUID      string         `json:"target_uid"`
	ScriptUID      string         `json:"script_uid"`
	Path           string         `json:"path"`
	Exists         bool           `json:"exists"`
	Managed        bool           `json:"managed"`
	WillBackup     bool           `json:"will_backup"`
	Changed        bool           `json:"changed"`
	MatchRule      jump.MatchRule `json:"match_rule"`
	MatchSummary   string         `json:"match_summary"`
	ScriptRedacted string         `json:"script_redacted"`
}

type diagnosticsResponse struct {
	ConfigDir          string             `json:"config_dir"`
	ProfilesDir        string             `json:"profiles_dir"`
	ProfilesYAML       string             `json:"profiles_yaml"`
	ConfigDirExists    bool               `json:"config_dir_exists"`
	ProfilesDirExists  bool               `json:"profiles_dir_exists"`
	ProfilesYAMLExists bool               `json:"profiles_yaml_exists"`
	StateFile          string             `json:"state_file"`
	StateFileExists    bool               `json:"state_file_exists"`
	BackupDir          string             `json:"backup_dir"`
	RemoteCount        int                `json:"remote_count"`
	Remotes            []diagnosticRemote `json:"remotes"`
}

type diagnosticRemote struct {
	UID                string               `json:"uid"`
	Name               string               `json:"name"`
	ScriptUID          string               `json:"script_uid"`
	ScriptExists       bool                 `json:"script_exists"`
	ScriptManaged      bool                 `json:"script_managed"`
	NodeCount          int                  `json:"node_count"`
	ServerDistribution []serverDistribution `json:"server_distribution"`
	IsJumpSource       bool                 `json:"is_jump_source"`
	IsJumpTarget       bool                 `json:"is_jump_target"`
	MatchSummary       string               `json:"match_summary"`
}

type serverDistribution struct {
	Server string `json:"server"`
	Count  int    `json:"count"`
}

type runtimeStatusResponse struct {
	Active            bool            `json:"active"`
	Reason            string          `json:"reason"`
	CurrentUID        string          `json:"current_uid"`
	CurrentName       string          `json:"current_name"`
	TargetUID         string          `json:"target_uid"`
	TargetName        string          `json:"target_name"`
	ScriptUID         string          `json:"script_uid"`
	ScriptPath        string          `json:"script_path"`
	ScriptExists      bool            `json:"script_exists"`
	ScriptKind        jump.ScriptKind `json:"script_kind"`
	ScriptManaged     bool            `json:"script_managed"`
	CanDisableForeign bool            `json:"can_disable_foreign"`
	SourceName        string          `json:"source_name"`
	SourceServer      string          `json:"source_server"`
	SourcePort        int             `json:"source_port"`
	SourceSubUID      string          `json:"source_sub_uid"`
	SourceSubName     string          `json:"source_sub_name"`
	MatchSummary      string          `json:"match_summary"`
}

func New(opts Options) (*App, error) {
	if opts.ConfigDir == "" {
		opts.ConfigDir = clash.DefaultConfigDir()
	}
	if opts.StateFile == "" {
		opts.StateFile = "state.json"
	}
	if opts.BackupDir == "" {
		opts.BackupDir = "backups"
	}
	state, err := store.LoadState(opts.StateFile)
	if err != nil {
		return nil, err
	}
	app := &App{
		client:    clash.NewClient(opts.ConfigDir),
		stateFile: opts.StateFile,
		backupDir: opts.BackupDir,
		staticFS:  opts.StaticFS,
		state:     state,
		mux:       http.NewServeMux(),
	}
	if err := app.reconcileStateFromManagedScripts(); err != nil {
		return nil, err
	}
	if err := app.normalizeAndSave(); err != nil {
		return nil, err
	}
	app.routes()
	return app, nil
}

func (a *App) Handler() http.Handler {
	return a.mux
}

func (a *App) routes() {
	a.mux.HandleFunc("GET /api/subscriptions", a.listSubscriptions)
	a.mux.HandleFunc("GET /api/subscriptions/{uid}/nodes", a.getSubscriptionNodes)
	a.mux.HandleFunc("GET /api/jump/state", a.getJumpState)
	a.mux.HandleFunc("PUT /api/jump/source", a.setJumpSource)
	a.mux.HandleFunc("DELETE /api/jump/source", a.clearJumpSource)
	a.mux.HandleFunc("PUT /api/jump/target/{uid}", a.addJumpTarget)
	a.mux.HandleFunc("DELETE /api/jump/target/{uid}", a.removeJumpTarget)
	a.mux.HandleFunc("PUT /api/jump/target/{uid}/rule", a.setJumpTargetRule)
	a.mux.HandleFunc("PUT /api/jump/target/{uid}/rule/server-port-all", a.setJumpTargetAllServerPorts)
	a.mux.HandleFunc("PUT /api/jump/toggle", a.toggleJump)
	a.mux.HandleFunc("GET /api/jump/preview", a.preview)
	a.mux.HandleFunc("POST /api/jump/preview", a.preview)
	a.mux.HandleFunc("GET /api/diagnostics", a.diagnostics)
	a.mux.HandleFunc("GET /api/runtime/status", a.runtimeStatus)
	a.mux.HandleFunc("POST /api/scripts/{uid}/disable-foreign-jump", a.disableForeignJump)
	a.mux.HandleFunc("POST /api/apply", a.apply)
	a.mux.HandleFunc("POST /api/reset", a.reset)
	if a.staticFS != nil {
		a.mux.Handle("/", http.FileServerFS(a.staticFS))
	}
}

func (a *App) listSubscriptions(w http.ResponseWriter, r *http.Request) {
	remotes, err := a.client.ListRemotes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for i := range remotes {
		remote := &remotes[i]
		remote.IsJumpSource = a.state.Source != nil && a.state.Source.SubUID == remote.UID
		remote.IsJumpTarget = contains(a.state.Targets, remote.UID)
		if rule, ok := a.state.TargetRules[remote.UID]; ok {
			remote.MatchRule = &rule
			remote.MatchSummary = RuleSummary(rule)
		}
	}
	writeJSON(w, http.StatusOK, remotes)
}

func (a *App) getSubscriptionNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := a.client.Nodes(r.PathValue("uid"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, nodes)
}

func (a *App) getJumpState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, jump.PublicState(a.state))
}

func (a *App) setJumpSource(w http.ResponseWriter, r *http.Request) {
	var req setSourceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	node, ok, err := a.client.FindNode(req.SubUID, req.NodeName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("Node %q not found in subscription %s", req.NodeName, req.SubUID))
		return
	}
	a.state.Source = &jump.JumpSource{SubUID: req.SubUID, Node: node}
	if err := a.saveStateAndInvalidatePreview(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) clearJumpSource(w http.ResponseWriter, r *http.Request) {
	a.state.Source = nil
	a.state.Targets = []string{}
	a.state.TargetRules = map[string]jump.MatchRule{}
	a.state.Enabled = false
	if err := a.saveStateAndInvalidatePreview(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) addJumpTarget(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	if !a.remoteExists(uid) {
		writeError(w, http.StatusNotFound, fmt.Errorf("Subscription %s not found", uid))
		return
	}
	if !contains(a.state.Targets, uid) {
		a.state.Targets = append(a.state.Targets, uid)
	}
	rule := a.defaultMatchRuleForTarget(uid)
	a.state.TargetRules[uid] = rule
	if err := a.saveStateAndInvalidatePreview(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "match_rule": rule})
}

func (a *App) removeJumpTarget(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	a.state.Targets = removeString(a.state.Targets, uid)
	delete(a.state.TargetRules, uid)
	if err := a.saveStateAndInvalidatePreview(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) setJumpTargetRule(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	if !contains(a.state.Targets, uid) {
		writeError(w, http.StatusNotFound, fmt.Errorf("Subscription %s is not a jump target", uid))
		return
	}
	var req setTargetRuleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	a.state.TargetRules[uid] = req.Rule
	if err := a.saveStateAndInvalidatePreview(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "match_rule": req.Rule})
}

func (a *App) setJumpTargetAllServerPorts(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	if !contains(a.state.Targets, uid) {
		writeError(w, http.StatusNotFound, fmt.Errorf("Subscription %s is not a jump target", uid))
		return
	}
	rule := a.allServerPortRule(uid)
	a.state.TargetRules[uid] = rule
	if err := a.saveStateAndInvalidatePreview(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "match_rule": rule})
}

func (a *App) toggleJump(w http.ResponseWriter, r *http.Request) {
	var req toggleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	a.state.Enabled = req.Enabled
	if err := a.saveStateAndInvalidatePreview(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) preview(w http.ResponseWriter, r *http.Request) {
	issueToken := r.URL.Query().Get("token") != "0"
	baseState := a.state
	if r.Method == http.MethodPost {
		var req previewRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if req.State == nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("preview state is required"))
			return
		}
		baseState = *req.State
	}
	resp, err := a.previewScripts(baseState, issueToken)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) diagnostics(w http.ResponseWriter, r *http.Request) {
	resp, err := a.diagnosticsData()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) runtimeStatus(w http.ResponseWriter, r *http.Request) {
	resp, err := a.runtimeStatusData()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) disableForeignJump(w http.ResponseWriter, r *http.Request) {
	var req disableForeignJumpRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !req.Confirm {
		writeError(w, http.StatusConflict, fmt.Errorf("该脚本不是本工具生成，停用前必须确认已备份风险"))
		return
	}
	scriptUID := r.PathValue("uid")
	action, err := store.DisableForeignJumpScript(filepath.Join(a.client.ProfilesDir, scriptUID+".js"), a.backupDir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"action":  action,
		"message": "第三方跳板脚本已备份并停用，可继续使用本工具重新设置",
	})
}

func (a *App) apply(w http.ResponseWriter, r *http.Request) {
	var req applyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusConflict, fmt.Errorf("请先预览脚本，再确认应用"))
		return
	}
	if req.PreviewToken == "" || req.PreviewToken != a.previewToken {
		writeError(w, http.StatusConflict, fmt.Errorf("请先预览脚本，再确认应用"))
		return
	}
	applyState := a.state
	if a.previewState != nil {
		applyState = *a.previewState
	}
	actions, err := a.applyScripts(applyState)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	a.previewToken = ""
	a.previewState = nil
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"actions": actions,
		"message": "跳板脚本已写入并备份原脚本，请在 Clash Verge 中刷新对应订阅",
	})
}

func (a *App) reset(w http.ResponseWriter, r *http.Request) {
	items, err := a.client.ProfileItems()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	actions, err := store.ResetManagedScripts(items, a.client.ProfilesDir, a.backupDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	a.state = jump.State{Targets: []string{}, TargetRules: map[string]jump.MatchRule{}}
	if err := a.saveStateAndInvalidatePreview(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"actions": actions,
		"message": "本工具管理的跳板脚本已清除",
	})
}

func (a *App) previewScripts(baseState jump.State, issueToken bool) (previewResponse, error) {
	previewState := baseState
	jump.NormalizeState(&previewState, a.defaultMatchRuleForTarget)
	if previewState.Source == nil {
		a.previewToken = ""
		a.previewState = nil
		return previewResponse{Ready: false, Reason: "请先选择跳板源", Items: []previewItem{}, State: jump.PublicState(previewState)}, nil
	}
	sourceNode, resolvedState, err := a.resolveSourceNodeForState(previewState)
	if err != nil {
		a.previewToken = ""
		a.previewState = nil
		return previewResponse{Ready: false, Reason: err.Error(), Items: []previewItem{}, State: jump.PublicState(previewState)}, nil
	}
	previewState = resolvedState
	items := []previewItem{}
	changeCount := 0
	targetScripts, err := a.targetScriptUIDsForState(previewState)
	if err != nil {
		return previewResponse{}, err
	}
	for _, targetUID := range sortedKeys(targetScripts) {
		scriptUID := targetScripts[targetUID]
		jsPath := filepath.Join(a.client.ProfilesDir, scriptUID+".js")
		existing, readErr := os.ReadFile(jsPath)
		if readErr != nil && !os.IsNotExist(readErr) {
			return previewResponse{}, readErr
		}
		rule := a.matchRuleForState(&previewState, targetUID)
		jumpScript, err := jump.GenerateScript(sourceNode, rule)
		if err != nil {
			return previewResponse{}, err
		}
		redactedScript, err := jump.RedactedScript(sourceNode, rule)
		if err != nil {
			return previewResponse{}, err
		}
		exists := readErr == nil
		managed := jump.IsManagedScript(string(existing))
		desiredScript := jumpScript
		if !previewState.Enabled {
			desiredScript = jump.NoopScript()
			redactedScript = desiredScript
		}
		changed := false
		switch {
		case previewState.Enabled:
			changed = !exists || !sameScript(existing, desiredScript)
		case exists && managed:
			changed = !sameScript(existing, desiredScript)
		}
		if changed {
			changeCount++
		}
		items = append(items, previewItem{
			TargetUID:      targetUID,
			ScriptUID:      scriptUID,
			Path:           jsPath,
			Exists:         exists,
			Managed:        managed,
			WillBackup:     changed && exists,
			Changed:        changed,
			MatchRule:      rule,
			MatchSummary:   RuleSummary(rule),
			ScriptRedacted: redactedScript,
		})
	}
	ready := len(previewState.Targets) > 0
	reason := ""
	if !ready {
		reason = "请先选择目标订阅"
	}
	hasChanges := changeCount > 0
	token := ""
	if ready && hasChanges && issueToken {
		var err error
		token, err = newPreviewToken()
		if err != nil {
			return previewResponse{}, err
		}
		a.previewToken = token
		a.previewState = cloneState(previewState)
	} else if !ready || !hasChanges {
		a.previewToken = ""
		a.previewState = nil
	}
	return previewResponse{
		Ready:        ready,
		Reason:       reason,
		Enabled:      previewState.Enabled,
		HasChanges:   hasChanges,
		ChangeCount:  changeCount,
		PreviewToken: token,
		Items:        items,
		State:        jump.PublicState(previewState),
	}, nil
}

func (a *App) applyScripts(state jump.State) ([]store.Action, error) {
	applyState := state
	jump.NormalizeState(&applyState, a.defaultMatchRuleForTarget)
	if applyState.Source == nil || len(applyState.Targets) == 0 {
		items, err := a.client.ProfileItems()
		if err != nil {
			return nil, err
		}
		actions, err := store.ResetManagedScripts(items, a.client.ProfilesDir, a.backupDir)
		if err != nil {
			return nil, err
		}
		a.state = applyState
		return actions, a.saveState()
	}
	sourceNode, resolvedState, err := a.resolveSourceNodeForState(applyState)
	if err != nil {
		return nil, err
	}
	applyState = resolvedState
	targetScripts, err := a.targetScriptUIDsForState(applyState)
	if err != nil {
		return nil, err
	}
	active := map[string]bool{}
	if applyState.Enabled {
		for _, scriptUID := range targetScripts {
			active[scriptUID] = true
		}
	}
	items, err := a.client.ProfileItems()
	if err != nil {
		return nil, err
	}
	actions := []store.Action{}
	for _, item := range items {
		if item.Type != "script" {
			continue
		}
		jsPath := filepath.Join(a.client.ProfilesDir, item.UID+".js")
		raw, err := os.ReadFile(jsPath)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if !active[item.UID] && len(raw) > 0 && jump.IsManagedScript(string(raw)) {
			backupPath, err := store.WriteScriptWithBackup(jsPath, a.backupDir, "disable", jump.NoopScript())
			if err != nil {
				return nil, err
			}
			actions = append(actions, store.Action{Action: "disabled", ScriptUID: item.UID, Path: jsPath, BackupPath: backupPath})
		}
	}
	if !applyState.Enabled {
		a.state = applyState
		return actions, a.saveState()
	}
	for targetUID, scriptUID := range targetScripts {
		rule := a.matchRuleForState(&applyState, targetUID)
		content, err := jump.GenerateScript(sourceNode, rule)
		if err != nil {
			return nil, err
		}
		jsPath := filepath.Join(a.client.ProfilesDir, scriptUID+".js")
		backupPath, err := store.WriteScriptWithBackup(jsPath, a.backupDir, "apply", content)
		if err != nil {
			return nil, err
		}
		actions = append(actions, store.Action{
			Action:     "applied",
			TargetUID:  targetUID,
			ScriptUID:  scriptUID,
			Path:       jsPath,
			BackupPath: backupPath,
			MatchRule:  rule,
		})
	}
	a.state = applyState
	return actions, a.saveState()
}

func (a *App) resolveSourceNode() (jump.JumpNode, error) {
	if a.state.Source == nil {
		return jump.JumpNode{}, fmt.Errorf("请先选择跳板源")
	}
	source := a.state.Source
	if source.SubUID != "" {
		node, ok, err := a.client.FindNode(source.SubUID, source.Node.Name)
		if err != nil {
			return jump.JumpNode{}, err
		}
		if ok {
			return node, nil
		}
	}
	remotes, err := a.client.ListRemotes()
	if err != nil {
		return jump.JumpNode{}, err
	}
	for _, remote := range remotes {
		nodes, err := a.client.Nodes(remote.UID)
		if err != nil {
			continue
		}
		for _, node := range nodes {
			if node.Server == source.Node.Server && node.Port == source.Node.Port {
				a.state.Source = &jump.JumpSource{SubUID: remote.UID, Node: node}
				_ = a.saveState()
				return node, nil
			}
		}
	}
	return jump.JumpNode{}, fmt.Errorf("无法从订阅缓存中恢复跳板节点，请重新选择跳板源")
}

func (a *App) resolveSourceNodeForState(state jump.State) (jump.JumpNode, jump.State, error) {
	if state.Source == nil {
		return jump.JumpNode{}, state, fmt.Errorf("请先选择跳板源")
	}
	source := state.Source
	if source.SubUID != "" {
		node, ok, err := a.findSourceNodeInSubscription(source.SubUID, source.Node)
		if err != nil {
			return jump.JumpNode{}, state, err
		}
		if ok {
			return node, state, nil
		}
	}
	remotes, err := a.client.ListRemotes()
	if err != nil {
		return jump.JumpNode{}, state, err
	}
	for _, remote := range remotes {
		nodes, err := a.client.Nodes(remote.UID)
		if err != nil {
			continue
		}
		for _, node := range nodes {
			if node.Server == source.Node.Server && node.Port == source.Node.Port {
				state.Source = &jump.JumpSource{SubUID: remote.UID, Node: node}
				return node, state, nil
			}
		}
	}
	return jump.JumpNode{}, state, fmt.Errorf("无法从订阅缓存中恢复跳板节点，请重新选择跳板源")
}

func (a *App) findSourceNodeInSubscription(subUID string, source jump.JumpNode) (jump.JumpNode, bool, error) {
	nodes, err := a.client.Nodes(subUID)
	if err != nil {
		return jump.JumpNode{}, false, err
	}
	for _, node := range nodes {
		if sourceNodeMatches(node, source, true) {
			return node, true, nil
		}
	}
	for _, node := range nodes {
		if sourceNodeMatches(node, source, false) {
			return node, true, nil
		}
	}
	for _, node := range nodes {
		if source.Name != "" && node.Name == source.Name {
			return node, true, nil
		}
	}
	return jump.JumpNode{}, false, nil
}

func (a *App) targetScriptUIDs() (map[string]string, error) {
	result := map[string]string{}
	for _, targetUID := range a.state.Targets {
		scriptUID, ok, err := a.client.ScriptUIDForRemote(targetUID)
		if err != nil {
			return nil, err
		}
		if ok {
			result[targetUID] = scriptUID
		}
	}
	return result, nil
}

func (a *App) targetScriptUIDsForState(state jump.State) (map[string]string, error) {
	result := map[string]string{}
	for _, targetUID := range state.Targets {
		scriptUID, ok, err := a.client.ScriptUIDForRemote(targetUID)
		if err != nil {
			return nil, err
		}
		if ok {
			result[targetUID] = scriptUID
		}
	}
	return result, nil
}

func (a *App) diagnosticsData() (diagnosticsResponse, error) {
	remotes, err := a.client.ListRemotes()
	if err != nil {
		return diagnosticsResponse{}, err
	}
	diagnosticRemotes := []diagnosticRemote{}
	for _, remote := range remotes {
		nodes, err := a.client.Nodes(remote.UID)
		if err != nil {
			return diagnosticsResponse{}, err
		}
		scriptPath := ""
		if remote.ScriptUID != "" {
			scriptPath = filepath.Join(a.client.ProfilesDir, remote.ScriptUID+".js")
		}
		scriptExists := false
		scriptManaged := false
		if scriptPath != "" {
			raw, err := os.ReadFile(scriptPath)
			if err == nil {
				scriptExists = true
				scriptManaged = jump.IsManagedScript(string(raw))
			} else if !os.IsNotExist(err) {
				return diagnosticsResponse{}, err
			}
		}
		rule := a.state.TargetRules[remote.UID]
		diagnosticRemotes = append(diagnosticRemotes, diagnosticRemote{
			UID:                remote.UID,
			Name:               remote.Name,
			ScriptUID:          remote.ScriptUID,
			ScriptExists:       scriptExists,
			ScriptManaged:      scriptManaged,
			NodeCount:          len(nodes),
			ServerDistribution: serverDistributionFor(nodes),
			IsJumpSource:       a.state.Source != nil && a.state.Source.SubUID == remote.UID,
			IsJumpTarget:       contains(a.state.Targets, remote.UID),
			MatchSummary:       RuleSummary(rule),
		})
	}
	profilesYAML := filepath.Join(a.client.ConfigDir, "profiles.yaml")
	return diagnosticsResponse{
		ConfigDir:          a.client.ConfigDir,
		ProfilesDir:        a.client.ProfilesDir,
		ProfilesYAML:       profilesYAML,
		ConfigDirExists:    exists(a.client.ConfigDir),
		ProfilesDirExists:  exists(a.client.ProfilesDir),
		ProfilesYAMLExists: exists(profilesYAML),
		StateFile:          a.stateFile,
		StateFileExists:    exists(a.stateFile),
		BackupDir:          a.backupDir,
		RemoteCount:        len(diagnosticRemotes),
		Remotes:            diagnosticRemotes,
	}, nil
}

func (a *App) runtimeStatusData() (runtimeStatusResponse, error) {
	profiles, err := a.client.ReadProfiles()
	if err != nil {
		return runtimeStatusResponse{}, err
	}
	resp := runtimeStatusResponse{
		CurrentUID: profiles.Current,
		ScriptKind: jump.ScriptKindNone,
	}
	if profiles.Current == "" {
		resp.Reason = "未检测到 Clash Verge 当前订阅"
		return resp, nil
	}
	current, ok := profileItemByUID(profiles.Items, profiles.Current)
	if !ok || current.Type != "remote" {
		resp.Reason = "当前订阅不在 profiles.yaml 的 remote 列表中"
		return resp, nil
	}
	resp.CurrentName = profileDisplayName(current)
	resp.TargetUID = current.UID
	resp.TargetName = resp.CurrentName
	resp.ScriptUID = current.Option.Script
	if resp.ScriptUID == "" {
		resp.Reason = "当前订阅未绑定脚本"
		return resp, nil
	}
	resp.ScriptPath = filepath.Join(a.client.ProfilesDir, resp.ScriptUID+".js")
	raw, err := os.ReadFile(resp.ScriptPath)
	if err != nil {
		if os.IsNotExist(err) {
			resp.Reason = "当前订阅绑定的脚本文件不存在"
			return resp, nil
		}
		return runtimeStatusResponse{}, err
	}
	resp.ScriptExists = true
	inspection := jump.InspectScript(string(raw))
	resp.ScriptKind = inspection.Kind
	resp.ScriptManaged = inspection.Managed
	resp.CanDisableForeign = inspection.Kind == jump.ScriptKindForeignJump
	resp.MatchSummary = inspection.MatchSummary
	if inspection.Source != nil {
		resp.SourceName = inspection.Source.Name
		resp.SourceServer = inspection.Source.Server
		resp.SourcePort = int(inspection.Source.Port)
		resp.SourceSubUID, resp.SourceSubName = a.findSourceSubscription(*inspection.Source)
	}
	switch inspection.Kind {
	case jump.ScriptKindManagedJump:
		resp.Active = true
		resp.Reason = "当前订阅正在使用本工具生成的跳板脚本"
	case jump.ScriptKindForeignJump:
		resp.Active = true
		resp.Reason = "当前订阅正在使用第三方或手工跳板脚本"
	case jump.ScriptKindManagedNoop:
		resp.Reason = "当前订阅脚本由本工具管理，但当前未注入跳板规则"
	case jump.ScriptKindNormal:
		resp.Reason = "当前订阅绑定了普通脚本，未检测到跳板规则"
	default:
		resp.Reason = "当前订阅脚本为空"
	}
	return resp, nil
}

func (a *App) reconcileStateFromManagedScripts() error {
	reconciled, foundManagedScript, err := a.actualManagedScriptState()
	if err != nil {
		return err
	}
	if !foundManagedScript {
		return nil
	}
	a.state = reconciled
	return nil
}

func (a *App) actualManagedScriptState() (jump.State, bool, error) {
	state := jump.State{Targets: []string{}, TargetRules: map[string]jump.MatchRule{}}
	remotes, err := a.client.ListRemotes()
	if err != nil {
		return state, false, err
	}
	foundManagedScript := false
	for _, remote := range remotes {
		if remote.ScriptUID == "" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(a.client.ProfilesDir, remote.ScriptUID+".js"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return state, false, err
		}
		inspection := jump.InspectScript(string(raw))
		if inspection.Managed {
			foundManagedScript = true
		}
		if inspection.Kind != jump.ScriptKindManagedJump {
			continue
		}
		state.Enabled = true
		state.Targets = append(state.Targets, remote.UID)
		if inspection.MatchRule != nil {
			state.TargetRules[remote.UID] = *inspection.MatchRule
		}
		if state.Source == nil && inspection.Source != nil {
			source := *inspection.Source
			subUID, _ := a.findSourceSubscription(source)
			state.Source = &jump.JumpSource{SubUID: subUID, Node: source}
		}
	}
	return state, foundManagedScript, nil
}

func (a *App) normalizeAndSave() error {
	jump.NormalizeState(&a.state, a.defaultMatchRuleForTarget)
	return a.saveState()
}

func (a *App) saveState() error {
	return store.SaveState(a.stateFile, a.state)
}

func (a *App) saveStateAndInvalidatePreview() error {
	a.previewToken = ""
	a.previewState = nil
	return a.saveState()
}

func (a *App) defaultMatchRuleForTarget(uid string) jump.MatchRule {
	nodes, err := a.client.Nodes(uid)
	if err != nil {
		return jump.MatchRule{Mode: "dominant_server"}
	}
	counts := map[string]int{}
	for _, node := range nodes {
		if node.Server != "" {
			counts[node.Server]++
		}
	}
	server := ""
	count := 0
	for candidate, candidateCount := range counts {
		if candidateCount > count || candidateCount == count && candidate < server {
			server = candidate
			count = candidateCount
		}
	}
	if server == "" {
		return jump.MatchRule{Mode: "dominant_server"}
	}
	return jump.MatchRule{Mode: "dominant_server", Servers: []string{server}}
}

func (a *App) allServerPortRule(uid string) jump.MatchRule {
	nodes, err := a.client.Nodes(uid)
	if err != nil {
		return jump.MatchRule{Mode: "server_port"}
	}
	seen := map[string]bool{}
	pairs := []jump.ServerPort{}
	for _, node := range nodes {
		if node.Server == "" || node.Port == 0 {
			continue
		}
		key := fmt.Sprintf("%s:%d", node.Server, node.Port)
		if seen[key] {
			continue
		}
		seen[key] = true
		pairs = append(pairs, jump.ServerPort{Server: node.Server, Port: int(node.Port)})
	}
	return jump.MatchRule{Mode: "server_port", ServerPorts: pairs}
}

func (a *App) matchRuleForTarget(uid string) jump.MatchRule {
	if rule, ok := a.state.TargetRules[uid]; ok {
		return rule
	}
	rule := a.defaultMatchRuleForTarget(uid)
	a.state.TargetRules[uid] = rule
	return rule
}

func (a *App) matchRuleForState(state *jump.State, uid string) jump.MatchRule {
	if state.TargetRules == nil {
		state.TargetRules = map[string]jump.MatchRule{}
	}
	if rule, ok := state.TargetRules[uid]; ok {
		return rule
	}
	rule := a.defaultMatchRuleForTarget(uid)
	state.TargetRules[uid] = rule
	return rule
}

func (a *App) remoteExists(uid string) bool {
	remotes, err := a.client.ListRemotes()
	if err != nil {
		return false
	}
	for _, remote := range remotes {
		if remote.UID == uid {
			return true
		}
	}
	return false
}

func RuleSummary(rule jump.MatchRule) string {
	switch rule.Mode {
	case "server_port":
		return fmt.Sprintf("server+port x%d", len(rule.ServerPorts))
	case "manual":
		return fmt.Sprintf("manual nodes x%d", len(rule.NodeNames))
	case "", "dominant_server", "server":
		if len(rule.Servers) == 0 {
			return ""
		}
		return fmt.Sprintf("server x%d", len(rule.Servers))
	default:
		return fmt.Sprintf("%s x%d", rule.Mode, len(rule.Servers))
	}
}

func serverDistributionFor(nodes []jump.JumpNode) []serverDistribution {
	counts := map[string]int{}
	for _, node := range nodes {
		if node.Server != "" {
			counts[node.Server]++
		}
	}
	servers := make([]string, 0, len(counts))
	for server := range counts {
		servers = append(servers, server)
	}
	sort.Slice(servers, func(i, j int) bool {
		if counts[servers[i]] == counts[servers[j]] {
			return servers[i] < servers[j]
		}
		return counts[servers[i]] > counts[servers[j]]
	})
	result := make([]serverDistribution, 0, len(servers))
	for _, server := range servers {
		result = append(result, serverDistribution{Server: server, Count: counts[server]})
	}
	return result
}

func (a *App) findSourceSubscription(source jump.JumpNode) (string, string) {
	remotes, err := a.client.ListRemotes()
	if err != nil {
		return "", ""
	}
	fallbackUID := ""
	fallbackName := ""
	for _, remote := range remotes {
		nodes, err := a.client.Nodes(remote.UID)
		if err != nil {
			continue
		}
		for _, node := range nodes {
			if sourceNodeMatches(node, source, true) {
				return remote.UID, remote.Name
			}
			if fallbackUID == "" && sourceNodeMatches(node, source, false) {
				fallbackUID = remote.UID
				fallbackName = remote.Name
			}
		}
	}
	return fallbackUID, fallbackName
}

func sourceNodeMatches(node jump.JumpNode, source jump.JumpNode, includeSensitive bool) bool {
	nameMatches := source.Name != "" && node.Name == source.Name
	endpointMatches := source.Server != "" && node.Server == source.Server && source.Port != 0 && node.Port == source.Port
	if !nameMatches && !endpointMatches {
		return false
	}
	if !includeSensitive {
		return true
	}
	checks := []struct {
		source string
		node   string
	}{
		{source.Password, node.Password},
		{source.UUID, node.UUID},
		{source.SNI, node.SNI},
		{source.Up, node.Up},
		{source.Down, node.Down},
		{source.Cipher, node.Cipher},
		{source.Network, node.Network},
		{source.ClientFingerprint, node.ClientFingerprint},
		{source.Flow, node.Flow},
		{source.ServerName, node.ServerName},
		{source.Plugin, node.Plugin},
	}
	hasExactHint := false
	for _, check := range checks {
		if check.source == "" {
			continue
		}
		hasExactHint = true
		if check.source != check.node {
			return false
		}
	}
	if source.AlterID != 0 {
		hasExactHint = true
		if source.AlterID != node.AlterID {
			return false
		}
	}
	if source.SkipCertVerify {
		hasExactHint = true
		if source.SkipCertVerify != node.SkipCertVerify {
			return false
		}
	}
	if source.UDP {
		hasExactHint = true
		if source.UDP != node.UDP {
			return false
		}
	}
	if source.TLS {
		hasExactHint = true
		if source.TLS != node.TLS {
			return false
		}
	}
	return hasExactHint
}

func profileItemByUID(items []clash.ProfileItem, uid string) (clash.ProfileItem, bool) {
	for _, item := range items {
		if item.UID == uid {
			return item, true
		}
	}
	return clash.ProfileItem{}, false
}

func profileDisplayName(item clash.ProfileItem) string {
	if item.Name != "" {
		return item.Name
	}
	return item.UID
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"detail": err.Error()})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	if err := json.NewDecoder(r.Body).Decode(value); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return false
	}
	return true
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func removeString(items []string, value string) []string {
	result := items[:0]
	for _, item := range items {
		if item != value {
			result = append(result, item)
		}
	}
	return result
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func sameScript(existing []byte, desired string) bool {
	return normalizeScript(string(existing)) == normalizeScript(desired)
}

func normalizeScript(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return strings.TrimSpace(content)
}

func cloneState(state jump.State) *jump.State {
	cloned := state
	if state.Source != nil {
		source := *state.Source
		cloned.Source = &source
	}
	if state.Targets != nil {
		cloned.Targets = append([]string{}, state.Targets...)
	}
	if state.TargetRules != nil {
		cloned.TargetRules = map[string]jump.MatchRule{}
		for key, rule := range state.TargetRules {
			cloned.TargetRules[key] = cloneMatchRule(rule)
		}
	}
	return &cloned
}

func cloneMatchRule(rule jump.MatchRule) jump.MatchRule {
	if rule.Servers != nil {
		rule.Servers = append([]string{}, rule.Servers...)
	}
	if rule.ServerPorts != nil {
		rule.ServerPorts = append([]jump.ServerPort{}, rule.ServerPorts...)
	}
	if rule.NodeNames != nil {
		rule.NodeNames = append([]string{}, rule.NodeNames...)
	}
	return rule
}

func newPreviewToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
