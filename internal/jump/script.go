package jump

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type ScriptKind string

const (
	ScriptKindNone        ScriptKind = "none"
	ScriptKindNormal      ScriptKind = "normal"
	ScriptKindManagedJump ScriptKind = "managed_jump"
	ScriptKindManagedNoop ScriptKind = "managed_noop"
	ScriptKindForeignJump ScriptKind = "foreign_jump"
)

type ScriptInspection struct {
	Kind           ScriptKind `json:"kind"`
	Managed        bool       `json:"managed"`
	HasDialerProxy bool       `json:"has_dialer_proxy"`
	Source         *JumpNode  `json:"source,omitempty"`
	MatchRule      *MatchRule `json:"match_rule,omitempty"`
	MatchSummary   string     `json:"match_summary,omitempty"`
}

func IsManagedScript(content string) bool {
	return strings.Contains(content, ManagedMarker) || strings.Contains(content, LegacyMarker)
}

func InspectScript(content string) ScriptInspection {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ScriptInspection{Kind: ScriptKindNone}
	}
	managed := IsManagedScript(content)
	hasDialerProxy := strings.Contains(content, "dialer-proxy")
	hasJumpMutation := hasDialerProxy || strings.Contains(content, "config.proxies.unshift")
	matchRule := inspectMatchRule(content)
	result := ScriptInspection{
		Kind:           ScriptKindNormal,
		Managed:        managed,
		HasDialerProxy: hasDialerProxy,
		Source:         inspectJumpProxy(content),
		MatchRule:      matchRule,
		MatchSummary:   inspectMatchSummary(matchRule),
	}
	switch {
	case managed && hasDialerProxy:
		result.Kind = ScriptKindManagedJump
	case managed:
		result.Kind = ScriptKindManagedNoop
	case hasJumpMutation:
		result.Kind = ScriptKindForeignJump
	}
	return result
}

func NoopScript() string {
	return fmt.Sprintf(`// %s
// No active jump proxy rules.

function main(config, profileName) {
  return config;
}
`, ManagedMarker)
}

func PlainNoopScript() string {
	return `function main(config, profileName) {
  return config;
}
`
}

func RedactedScript(node JumpNode, rule MatchRule) (string, error) {
	return GenerateScript(RedactNode(node, true), rule)
}

func GenerateScript(node JumpNode, rule MatchRule) (string, error) {
	if rule.Mode == "" {
		rule.Mode = "dominant_server"
	}
	proxyName := node.Name
	jsObj, err := nodeToJSObject(node, 4)
	if err != nil {
		return "", err
	}
	setup, condition, err := generateMatchCondition(rule)
	if err != nil {
		return "", err
	}
	proxyNameJS, err := JSString(proxyName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`// %s
// Jump proxy: %s

function main(config, profileName) {
  config.mode = "rule";

  const jumpProxy = {
%s
  };

%s

  const idx = config.proxies.findIndex(p => p.name === %s);
  if (idx >= 0) config.proxies.splice(idx, 1);
  config.proxies.unshift(jumpProxy);

  config.proxies.forEach(proxy => {
    if (%s) {
      proxy["dialer-proxy"] = %s;
    }
  });

  return config;
}
`, ManagedMarker, proxyName, jsObj, setup, proxyNameJS, condition, proxyNameJS), nil
}

func nodeToJSObject(node JumpNode, indent int) (string, error) {
	pad := strings.Repeat(" ", indent)
	fields := []struct {
		key   string
		value any
	}{
		{"name", node.Name},
		{"type", node.Type},
		{"server", node.Server},
		{"port", node.Port},
	}
	lowerType := strings.ToLower(node.Type)
	if node.Password != "" {
		fields = append(fields, struct {
			key   string
			value any
		}{"password", node.Password})
	}
	if node.UUID != "" {
		fields = append(fields, struct {
			key   string
			value any
		}{"uuid", node.UUID})
	}
	if lowerType == "vmess" || node.AlterID != 0 {
		fields = append(fields, struct {
			key   string
			value any
		}{"alterId", node.AlterID})
	}
	if node.Cipher != "" {
		fields = append(fields, struct {
			key   string
			value any
		}{"cipher", node.Cipher})
	}
	if node.SNI != "" {
		fields = append(fields, struct {
			key   string
			value any
		}{"sni", node.SNI})
	}
	if node.ServerName != "" {
		fields = append(fields, struct {
			key   string
			value any
		}{"servername", node.ServerName})
	}
	if node.Network != "" {
		fields = append(fields, struct {
			key   string
			value any
		}{"network", node.Network})
	}
	if node.WSOpts != nil {
		fields = append(fields, struct {
			key   string
			value any
		}{"ws-opts", node.WSOpts})
	}
	if node.TLS {
		fields = append(fields, struct {
			key   string
			value any
		}{"tls", node.TLS})
	}
	if node.ClientFingerprint != "" {
		fields = append(fields, struct {
			key   string
			value any
		}{"client-fingerprint", node.ClientFingerprint})
	}
	if len(node.ALPN) > 0 {
		fields = append(fields, struct {
			key   string
			value any
		}{"alpn", node.ALPN})
	}
	if node.Flow != "" {
		fields = append(fields, struct {
			key   string
			value any
		}{"flow", node.Flow})
	}
	if node.RealityOpts != nil {
		fields = append(fields, struct {
			key   string
			value any
		}{"reality-opts", node.RealityOpts})
	}
	if node.Plugin != "" {
		fields = append(fields, struct {
			key   string
			value any
		}{"plugin", node.Plugin})
	}
	if node.PluginOpts != nil {
		fields = append(fields, struct {
			key   string
			value any
		}{"plugin-opts", node.PluginOpts})
	}
	if node.Up != "" {
		fields = append(fields, struct {
			key   string
			value any
		}{"up", node.Up})
	}
	if node.Down != "" {
		fields = append(fields, struct {
			key   string
			value any
		}{"down", node.Down})
	}
	fields = append(fields,
		struct {
			key   string
			value any
		}{"udp", node.UDP},
		struct {
			key   string
			value any
		}{"skip-cert-verify", node.SkipCertVerify},
	)

	lines := make([]string, 0, len(fields))
	for _, field := range fields {
		value, err := JSString(field.value)
		if err != nil {
			return "", err
		}
		lines = append(lines, fmt.Sprintf("%s%s: %s,", pad, jsKey(field.key), value))
	}
	return strings.Join(lines, "\n"), nil
}

func generateMatchCondition(rule MatchRule) (string, string, error) {
	switch rule.Mode {
	case "server_port":
		if len(rule.ServerPorts) == 0 {
			return "", "", fmt.Errorf("目标匹配规则为空，请重新设置目标订阅")
		}
		endpoints := make([]string, 0, len(rule.ServerPorts))
		for _, item := range rule.ServerPorts {
			endpoints = append(endpoints, fmt.Sprintf("%s:%d", item.Server, item.Port))
		}
		values, err := JSString(endpoints)
		if err != nil {
			return "", "", err
		}
		return "  const targetEndpoints = new Set(" + values + ");", "targetEndpoints.has(`${proxy.server}:${proxy.port}`)", nil
	case "manual":
		if len(rule.NodeNames) == 0 {
			return "", "", fmt.Errorf("目标匹配规则为空，请重新设置目标订阅")
		}
		values, err := JSString(rule.NodeNames)
		if err != nil {
			return "", "", err
		}
		return "  const targetNames = new Set(" + values + ");", "targetNames.has(proxy.name)", nil
	default:
		if len(rule.Servers) == 0 {
			return "", "", fmt.Errorf("目标匹配规则为空，请重新设置目标订阅")
		}
		values, err := JSString(rule.Servers)
		if err != nil {
			return "", "", err
		}
		return "  const targetServers = new Set(" + values + ");", "targetServers.has(proxy.server)", nil
	}
}

var jsIdentifierRE = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

func jsKey(key string) string {
	if jsIdentifierRE.MatchString(key) {
		return key
	}
	encoded, err := JSString(key)
	if err != nil {
		return `"` + key + `"`
	}
	return encoded
}

var jumpProxyRE = regexp.MustCompile(`(?s)const\s+jumpProxy\s*=\s*\{(.*?)\n\s*\};`)

func inspectJumpProxy(content string) *JumpNode {
	match := jumpProxyRE.FindStringSubmatch(content)
	if len(match) != 2 {
		return nil
	}
	body := match[1]
	node := JumpNode{
		Name:              inspectJSONStringField(body, "name"),
		Type:              inspectJSONStringField(body, "type"),
		Server:            inspectJSONStringField(body, "server"),
		Port:              Port(inspectIntField(body, "port")),
		Password:          inspectJSONStringField(body, "password"),
		UUID:              inspectJSONStringField(body, "uuid"),
		SNI:               inspectJSONStringField(body, "sni"),
		SkipCertVerify:    inspectBoolField(body, "skip-cert-verify"),
		Up:                inspectJSONStringField(body, "up"),
		Down:              inspectJSONStringField(body, "down"),
		Cipher:            inspectJSONStringField(body, "cipher"),
		AlterID:           inspectIntField(body, "alterId"),
		UDP:               inspectBoolField(body, "udp"),
		Network:           inspectJSONStringField(body, "network"),
		TLS:               inspectBoolField(body, "tls"),
		ClientFingerprint: inspectJSONStringField(body, "client-fingerprint"),
		Flow:              inspectJSONStringField(body, "flow"),
		ServerName:        inspectJSONStringField(body, "servername"),
		Plugin:            inspectJSONStringField(body, "plugin"),
	}
	if node.Name == "" && node.Server == "" && node.Port == 0 {
		return nil
	}
	return &node
}

func inspectJSONStringField(body string, key string) string {
	value := inspectRawField(body, key)
	if value == "" {
		return ""
	}
	var decoded string
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return ""
	}
	return decoded
}

func inspectIntField(body string, key string) int {
	value := inspectRawField(body, key)
	if value == "" {
		return 0
	}
	value = strings.TrimSpace(value)
	var decoded int
	if err := json.Unmarshal([]byte(value), &decoded); err == nil {
		return decoded
	}
	parsed, _ := strconv.Atoi(value)
	return parsed
}

func inspectBoolField(body string, key string) bool {
	value := inspectRawField(body, key)
	if value == "" {
		return false
	}
	var decoded bool
	if err := json.Unmarshal([]byte(value), &decoded); err == nil {
		return decoded
	}
	return strings.TrimSpace(value) == "true"
}

func inspectRawField(body string, key string) string {
	pattern := fmt.Sprintf(`(?m)^\s*(?:"%s"|%s)\s*:\s*(.+?),\s*$`, regexp.QuoteMeta(key), regexp.QuoteMeta(key))
	re := regexp.MustCompile(pattern)
	match := re.FindStringSubmatch(body)
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func inspectMatchRule(content string) *MatchRule {
	if values, ok := inspectStringSet(content, "targetEndpoints"); ok {
		serverPorts := make([]ServerPort, 0, len(values))
		for _, value := range values {
			idx := strings.LastIndex(value, ":")
			if idx <= 0 || idx == len(value)-1 {
				continue
			}
			port, err := strconv.Atoi(value[idx+1:])
			if err != nil {
				continue
			}
			serverPorts = append(serverPorts, ServerPort{Server: value[:idx], Port: port})
		}
		return &MatchRule{Mode: "server_port", ServerPorts: serverPorts}
	}
	if values, ok := inspectStringSet(content, "targetNames"); ok {
		return &MatchRule{Mode: "manual", NodeNames: values}
	}
	if values, ok := inspectStringSet(content, "targetServers"); ok {
		return &MatchRule{Mode: "server", Servers: values}
	}
	return nil
}

func inspectStringSet(content string, name string) ([]string, bool) {
	pattern := fmt.Sprintf(`(?s)%s\s*=\s*new\s+Set\((\[.*?\])\)`, regexp.QuoteMeta(name))
	re := regexp.MustCompile(pattern)
	match := re.FindStringSubmatch(content)
	if len(match) != 2 {
		return nil, false
	}
	var values []string
	if err := json.Unmarshal([]byte(match[1]), &values); err != nil {
		return nil, false
	}
	return values, true
}

func inspectMatchSummary(rule *MatchRule) string {
	if rule == nil {
		return ""
	}
	switch rule.Mode {
	case "server_port":
		return fmt.Sprintf("server+port x%d", len(rule.ServerPorts))
	case "manual":
		return fmt.Sprintf("manual nodes x%d", len(rule.NodeNames))
	case "server", "dominant_server":
		return fmt.Sprintf("server x%d", len(rule.Servers))
	}
	return ""
}
