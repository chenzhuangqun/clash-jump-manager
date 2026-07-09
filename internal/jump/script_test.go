package jump

import (
	"strings"
	"testing"
)

func TestGenerateScriptCanMatchExactServerPort(t *testing.T) {
	node := JumpNode{
		Name:           `jump "node"`,
		Type:           "hysteria2",
		Server:         "jump.example.com",
		Port:           443,
		Password:       `p"w`,
		SNI:            "jump.example.com",
		SkipCertVerify: true,
		UDP:            true,
	}
	rule := MatchRule{
		Mode:        "server_port",
		ServerPorts: []ServerPort{{Server: "1.2.3.4", Port: 4001}},
	}

	script, err := GenerateScript(node, rule)
	if err != nil {
		t.Fatalf("GenerateScript returned error: %v", err)
	}

	if !strings.Contains(script, "targetEndpoints") {
		t.Fatalf("expected server+port endpoint set in script:\n%s", script)
	}
	if !strings.Contains(script, `"1.2.3.4:4001"`) {
		t.Fatalf("expected endpoint literal in script:\n%s", script)
	}
	if !strings.Contains(script, "`${proxy.server}:${proxy.port}`") {
		t.Fatalf("expected server+port proxy condition in script:\n%s", script)
	}
	if !strings.Contains(script, `proxy["dialer-proxy"] = "jump \"node\""`) {
		t.Fatalf("expected escaped jump node name in script:\n%s", script)
	}
	if strings.Contains(script, `proxy.server === "1.2.3.4"`) {
		t.Fatalf("server_port rule must not fall back to server-only matching:\n%s", script)
	}
}

func TestRedactedScriptMasksSecretsAndSensitiveOptions(t *testing.T) {
	node := JumpNode{
		Name:        "jump",
		Type:        "vmess",
		Server:      "jump.example.com",
		Port:        443,
		Password:    "secret-password",
		UUID:        "secret-uuid",
		Network:     "ws",
		WSOpts:      map[string]any{"path": "/secret-path"},
		RealityOpts: map[string]any{"public-key": "secret-key"},
		PluginOpts:  map[string]any{"mode": "secret-plugin"},
	}
	rule := MatchRule{Mode: "server", Servers: []string{"1.2.3.4"}}

	script, err := RedactedScript(node, rule)
	if err != nil {
		t.Fatalf("RedactedScript returned error: %v", err)
	}

	for _, secret := range []string{"secret-password", "secret-uuid", "/secret-path", "secret-key", "secret-plugin"} {
		if strings.Contains(script, secret) {
			t.Fatalf("redacted script leaked %q:\n%s", secret, script)
		}
	}
	if strings.Count(script, `"***"`) < 5 {
		t.Fatalf("expected masked secret placeholders in script:\n%s", script)
	}
}

func TestGenerateScriptRejectsEmptyServerRule(t *testing.T) {
	_, err := GenerateScript(JumpNode{Name: "jump", Type: "hysteria2", Server: "jump.example.com", Port: 443}, MatchRule{Mode: "server"})
	if err == nil {
		t.Fatal("expected empty server rule to fail")
	}
}

func TestInspectScriptClassifiesManagedForeignAndNormalScripts(t *testing.T) {
	source := JumpNode{Name: `jump "node"`, Type: "hysteria2", Server: "jump.example.com", Port: 443}
	managedScript, err := GenerateScript(source, MatchRule{Mode: "server_port", ServerPorts: []ServerPort{{Server: "1.2.3.4", Port: 4001}}})
	if err != nil {
		t.Fatalf("GenerateScript returned error: %v", err)
	}

	managed := InspectScript(managedScript)
	if managed.Kind != ScriptKindManagedJump || !managed.Managed || !managed.HasDialerProxy {
		t.Fatalf("expected managed jump inspection, got %#v", managed)
	}
	if managed.Source == nil || managed.Source.Name != source.Name || managed.Source.Server != source.Server || managed.Source.Port != source.Port {
		t.Fatalf("expected managed source details, got %#v", managed.Source)
	}

	noop := InspectScript(NoopScript())
	if noop.Kind != ScriptKindManagedNoop || !noop.Managed || noop.HasDialerProxy {
		t.Fatalf("expected managed noop inspection, got %#v", noop)
	}

	foreign := InspectScript("function main(config) { config.proxies.forEach(p => p['dialer-proxy'] = 'jump'); return config; }")
	if foreign.Kind != ScriptKindForeignJump || foreign.Managed || !foreign.HasDialerProxy {
		t.Fatalf("expected foreign jump inspection, got %#v", foreign)
	}

	normal := InspectScript("function main(config) { config.mode = 'rule'; return config; }")
	if normal.Kind != ScriptKindNormal || normal.Managed || normal.HasDialerProxy {
		t.Fatalf("expected normal script inspection, got %#v", normal)
	}
}

func TestInspectScriptExtractsGeneratedMatchRule(t *testing.T) {
	source := JumpNode{Name: "jump", Type: "hysteria2", Server: "jump.example.com", Port: 443}

	serverPortScript, err := GenerateScript(source, MatchRule{
		Mode: "server_port",
		ServerPorts: []ServerPort{
			{Server: "1.2.3.4", Port: 4001},
			{Server: "5.6.7.8", Port: 4002},
		},
	})
	if err != nil {
		t.Fatalf("GenerateScript server_port returned error: %v", err)
	}
	serverPort := InspectScript(serverPortScript)
	if serverPort.MatchRule == nil || serverPort.MatchRule.Mode != "server_port" || len(serverPort.MatchRule.ServerPorts) != 2 {
		t.Fatalf("expected server_port match rule, got %#v", serverPort.MatchRule)
	}
	if serverPort.MatchRule.ServerPorts[1].Server != "5.6.7.8" || serverPort.MatchRule.ServerPorts[1].Port != 4002 {
		t.Fatalf("unexpected server_port values: %#v", serverPort.MatchRule.ServerPorts)
	}

	manualScript, err := GenerateScript(source, MatchRule{Mode: "manual", NodeNames: []string{"HK 01", "JP 02"}})
	if err != nil {
		t.Fatalf("GenerateScript manual returned error: %v", err)
	}
	manual := InspectScript(manualScript)
	if manual.MatchRule == nil || manual.MatchRule.Mode != "manual" || len(manual.MatchRule.NodeNames) != 2 {
		t.Fatalf("expected manual match rule, got %#v", manual.MatchRule)
	}

	serverScript, err := GenerateScript(source, MatchRule{Mode: "server", Servers: []string{"1.2.3.4"}})
	if err != nil {
		t.Fatalf("GenerateScript server returned error: %v", err)
	}
	server := InspectScript(serverScript)
	if server.MatchRule == nil || server.MatchRule.Mode != "server" || len(server.MatchRule.Servers) != 1 || server.MatchRule.Servers[0] != "1.2.3.4" {
		t.Fatalf("expected server match rule, got %#v", server.MatchRule)
	}
}
