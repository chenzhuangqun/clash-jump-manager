package main

import (
	"strings"
	"testing"
)

func TestEmbeddedWebLayoutConstrainsMainGridHeight(t *testing.T) {
	raw, err := embeddedWeb.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	html := string(raw)

	for _, required := range []string{
		".shell { max-width: 1180px; height: 100vh;",
		"flex: 1 1 auto; min-height: 0",
		".sidebar { min-height: 0;",
		".main { min-height: 0;",
		".nodes { min-height: 0;",
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("expected embedded layout CSS to contain %q", required)
		}
	}
}

func TestEmbeddedWebSummarySitsAboveGrid(t *testing.T) {
	raw, err := embeddedWeb.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	html := string(raw)
	summaryIndex := strings.Index(html, `<section class="summary-band">`)
	gridIndex := strings.Index(html, `<div class="grid">`)
	mainIndex := strings.Index(html, `<main class="main">`)
	if summaryIndex == -1 || gridIndex == -1 || mainIndex == -1 {
		t.Fatalf("expected summary, grid, and main in embedded index")
	}
	if !(summaryIndex < gridIndex && gridIndex < mainIndex) {
		t.Fatalf("expected summary before grid and outside main, got summary=%d grid=%d main=%d", summaryIndex, gridIndex, mainIndex)
	}
	if strings.Contains(html, "grid-template-rows: auto minmax(180px, 1fr) auto") {
		t.Fatal("main grid rows should not reserve a top summary row after summary moves above grid")
	}
}

func TestEmbeddedWebChoosesJumpSourceFromNodeSelection(t *testing.T) {
	raw, err := embeddedWeb.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	html := string(raw)
	for _, forbidden := range []string{"prompt(", "请输入跳板节点名称"} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("jump source selection should not use manual prompt %q", forbidden)
		}
	}
	for _, required := range []string{
		"onclick=\"selectNode(",
		"state.selectedNode",
		"请选择一个节点作为跳板源",
		"node.name === state.selectedNode",
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("expected embedded node selection flow to contain %q", required)
		}
	}
}

func TestEmbeddedWebShowsRuntimeStatusAndSwitchControl(t *testing.T) {
	raw, err := embeddedWeb.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	html := string(raw)
	for _, required := range []string{
		"class=\"switch\"",
		"class=\"switch-slider\"",
		"/api/runtime/status",
		"runtimeSummary",
		"loadRuntimeStatus",
		"disableForeignJump",
		"disable-foreign-jump",
		"confirm(",
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("expected embedded runtime/switch UI to contain %q", required)
		}
	}
	if strings.Contains(html, "<label><input type=\"checkbox\" id=\"toggleJump\" onchange=\"toggleJumpMode()\">") {
		t.Fatal("enable checkbox should use switch interaction markup")
	}
}

func TestEmbeddedWebShowsActualRuntimeStatusAfterTitle(t *testing.T) {
	raw, err := embeddedWeb.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	html := string(raw)
	for _, required := range []string{
		`<div class="brand-status">`,
		`<div class="status" title="Clash 当前实际脚本状态">`,
		"runtimeStatusLabel",
		"runtimeStatusIsActive",
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("expected actual runtime status layout to contain %q", required)
		}
	}
	titleIndex := strings.Index(html, `<h1>`)
	statusIndex := strings.Index(html, `<div class="status" title="Clash 当前实际脚本状态">`)
	switchIndex := strings.Index(html, `<label class="switch"`)
	if titleIndex == -1 || statusIndex == -1 || switchIndex == -1 {
		t.Fatalf("expected title, status, and switch in embedded index")
	}
	if !(titleIndex < statusIndex && statusIndex < switchIndex) {
		t.Fatalf("expected actual status after title and before desired-state switch, got title=%d status=%d switch=%d", titleIndex, statusIndex, switchIndex)
	}
	statusEnd := strings.Index(html[statusIndex:], `</div>`)
	if statusEnd == -1 {
		t.Fatal("expected status block to close")
	}
	statusBlock := html[statusIndex : statusIndex+statusEnd]
	if strings.Contains(statusBlock, "toggleJump") {
		t.Fatal("actual runtime status block should not contain the desired-state switch")
	}
	if strings.Contains(html, "document.getElementById('statusText').textContent = js.enabled ?") {
		t.Fatal("actual runtime status text should not be derived from unsaved desired state")
	}
}

func TestEmbeddedWebSeparatesSelectionActionsFromGlobalActions(t *testing.T) {
	raw, err := embeddedWeb.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	html := string(raw)
	for _, required := range []string{
		`<div class="global-actions">`,
		`<section class="selection-actions">`,
		`id="btnResetManaged"`,
		`恢复本工具脚本`,
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("expected separated action layout to contain %q", required)
		}
	}
	globalIndex := strings.Index(html, `<div class="global-actions">`)
	statusIndex := strings.Index(html, `<div class="status" title="Clash 当前实际脚本状态">`)
	switchIndex := strings.Index(html, `<label class="switch" title="切换本工具的待应用启用设置">`)
	summaryIndex := strings.Index(html, `<section class="summary-band">`)
	selectionIndex := strings.Index(html, `<section class="selection-actions">`)
	if globalIndex == -1 || statusIndex == -1 || switchIndex == -1 || summaryIndex == -1 || selectionIndex == -1 {
		t.Fatalf("expected status, desired switch, global actions, summary, and selection actions in embedded index")
	}
	if !(statusIndex < switchIndex && switchIndex < globalIndex && globalIndex < summaryIndex && summaryIndex < selectionIndex) {
		t.Fatalf("expected actual status after title, desired switch before actions, then summary and selection actions; got status=%d switch=%d global=%d summary=%d selection=%d", statusIndex, switchIndex, globalIndex, summaryIndex, selectionIndex)
	}
	selectionEnd := strings.Index(html[selectionIndex:], `</section>`)
	if selectionEnd == -1 {
		t.Fatal("expected selection actions section to close")
	}
	selectionBlock := html[selectionIndex : selectionIndex+selectionEnd]
	for _, forbidden := range []string{"loadRuntimeStatus()", "loadDiagnostics()", "loadPreview()", "applyConfig()", "resetConfig()"} {
		if strings.Contains(selectionBlock, forbidden) {
			t.Fatalf("selection actions should not contain global action %q", forbidden)
		}
	}
}

func TestEmbeddedWebUsesPlainTargetMatchLabels(t *testing.T) {
	raw, err := embeddedWeb.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	html := string(raw)
	for _, required := range []string{
		"加入目标",
		"匹配全部节点",
		"部分节点",
		"全部节点",
		"matchSummaryLabel",
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("expected plain target match copy to contain %q", required)
		}
	}
	for _, forbidden := range []string{
		"设为目标订阅",
		"精确匹配此订阅",
		"escapeHtml(sub.match_summary)",
		"selected?.match_summary || '-'",
		"escapeHtml(item.match_summary)",
		"escapeHtml(remote.match_summary",
	} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("expected user-facing copy to hide technical match label %q", forbidden)
		}
	}
}

func TestEmbeddedWebShowsGlobalOutputInModal(t *testing.T) {
	raw, err := embeddedWeb.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	html := string(raw)
	for _, required := range []string{
		`class="modal-backdrop"`,
		`class="modal"`,
		`id="modalBody"`,
		"showModal(",
		"closeModal()",
		"summary-band",
		"background: var(--panel2);",
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("expected modal summary layout to contain %q", required)
		}
	}
	for _, forbidden := range []string{
		"showPanel('检测状态'",
		"showPanel('写入预览'",
		"showPanel('诊断'",
	} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("global output should use modal instead of panel: found %q", forbidden)
		}
	}
}

func TestEmbeddedWebDisablesApplyWhenNoPendingChanges(t *testing.T) {
	raw, err := embeddedWeb.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	html := string(raw)
	for _, required := range []string{
		`id="btnApply"`,
		`disabled>应用配置</button>`,
		`button.success:disabled`,
		`/api/jump/preview?token=${issueToken ? '1' : '0'}`,
		`state.preview = preview`,
		`btnApply`,
		`has_changes`,
		`当前配置已是最新，无需应用`,
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("expected apply disabled flow to contain %q", required)
		}
	}
	if !strings.Contains(html, "document.getElementById('btnApply').disabled = !state.preview?.ready || !state.preview?.has_changes") {
		t.Fatal("apply button should be disabled unless preview reports pending changes")
	}
}

func TestEmbeddedWebUsesDraftStateUntilApply(t *testing.T) {
	raw, err := embeddedWeb.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	html := string(raw)
	for _, required := range []string{
		"draft: null",
		"appliedState: null",
		"cloneState(",
		"previewDraft(",
		"pendingSummary",
		"fetchDraftPreview",
		"method: 'POST'",
		"body: JSON.stringify({ state: state.draft })",
		"待应用",
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("expected draft-state UI flow to contain %q", required)
		}
	}
	for _, forbidden := range []string{
		"api('/api/jump/source',",
		"api(`/api/jump/target/${state.selectedSub}`, { method: 'PUT' })",
		"api(`/api/jump/target/${state.selectedSub}/rule/server-port-all`, { method: 'PUT' })",
		"api('/api/jump/toggle',",
	} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("selection and toggle changes should stay in local draft until apply: found %q", forbidden)
		}
	}
}

func TestEmbeddedWebSeparatesAppliedSummaryFromPendingDraft(t *testing.T) {
	raw, err := embeddedWeb.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("read embedded index: %v", err)
	}
	html := string(raw)
	for _, required := range []string{
		`grid-template-columns: repeat(3, minmax(0, 1fr))`,
		"已生效跳板源",
		"已生效目标订阅",
		"实际状态",
		"draftSummaryText",
		"刷新页面会回到已生效配置",
		"targetName && matchLabel",
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("expected applied summary/draft separation to contain %q", required)
		}
	}
	for _, forbidden := range []string{
		"已生效匹配",
		`id="ruleSummary"`,
		"document.getElementById('ruleSummary')",
		"当前匹配",
	} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("applied summary should not keep a separate match column: found %q", forbidden)
		}
	}
}
