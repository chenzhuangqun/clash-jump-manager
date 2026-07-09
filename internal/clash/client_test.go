package clash_test

import (
	"os"
	"path/filepath"
	"testing"

	"clash-jump-manager/internal/clash"
)

func TestClientListsRemotesAndFindsRealNodeSecrets(t *testing.T) {
	root := t.TempDir()
	profilesDir := filepath.Join(root, "profiles")
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}
	profilesYAML := `current: remote1
items:
  - type: remote
    uid: remote1
    name: Source Sub
    home: https://source.example/sub
    option:
      script: script1
    extra:
      upload: 100
      download: 200
      total: 1000
`
	if err := os.WriteFile(filepath.Join(root, "profiles.yaml"), []byte(profilesYAML), 0o644); err != nil {
		t.Fatalf("write profiles.yaml: %v", err)
	}
	subYAML := `proxies:
  - name: jump
    type: hysteria2
    server: jump.example.com
    port: 443
    password: secret-password
    sni: jump.example.com
    skip-cert-verify: true
`
	if err := os.WriteFile(filepath.Join(profilesDir, "remote1.yaml"), []byte(subYAML), 0o644); err != nil {
		t.Fatalf("write remote yaml: %v", err)
	}

	client := clash.NewClient(root)
	remotes, err := client.ListRemotes()
	if err != nil {
		t.Fatalf("ListRemotes returned error: %v", err)
	}
	if len(remotes) != 1 || remotes[0].UID != "remote1" || !remotes[0].Current || remotes[0].ScriptUID != "script1" {
		t.Fatalf("unexpected remotes: %#v", remotes)
	}
	if remotes[0].Traffic.Upload != 100 || remotes[0].Traffic.Download != 200 || remotes[0].Traffic.Total != 1000 {
		t.Fatalf("unexpected traffic: %#v", remotes[0].Traffic)
	}

	node, ok, err := client.FindNode("remote1", "jump")
	if err != nil {
		t.Fatalf("FindNode returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected to find jump node")
	}
	if node.Password != "secret-password" || node.SNI != "jump.example.com" || !node.SkipCertVerify {
		t.Fatalf("expected real node details from subscription cache, got %#v", node)
	}
}

func TestClientAcceptsStringPortsFromSubscriptionCache(t *testing.T) {
	root := t.TempDir()
	profilesDir := filepath.Join(root, "profiles")
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}
	subYAML := `proxies:
  - name: socks node
    type: socks5
    server: 127.0.0.1
    port: "1080"
`
	if err := os.WriteFile(filepath.Join(profilesDir, "remote1.yaml"), []byte(subYAML), 0o644); err != nil {
		t.Fatalf("write remote yaml: %v", err)
	}

	client := clash.NewClient(root)
	nodes, err := client.Nodes("remote1")
	if err != nil {
		t.Fatalf("Nodes returned error: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Port != 1080 {
		t.Fatalf("expected string port to parse as int, got %#v", nodes)
	}
}
