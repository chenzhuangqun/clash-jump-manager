package clash

import (
	"os"
	"path/filepath"

	"clash-jump-manager/internal/jump"
	"gopkg.in/yaml.v3"
)

type Client struct {
	ConfigDir   string
	ProfilesDir string
}

type ProfilesFile struct {
	Current string        `yaml:"current"`
	Items   []ProfileItem `yaml:"items"`
}

type ProfileItem struct {
	Type   string        `yaml:"type" json:"type"`
	UID    string        `yaml:"uid" json:"uid"`
	Name   string        `yaml:"name" json:"name"`
	Home   string        `yaml:"home" json:"home"`
	Option ProfileOption `yaml:"option" json:"option"`
	Extra  ProfileExtra  `yaml:"extra" json:"extra"`
}

type ProfileOption struct {
	Script string `yaml:"script" json:"script"`
}

type ProfileExtra struct {
	Upload   int64 `yaml:"upload" json:"upload"`
	Download int64 `yaml:"download" json:"download"`
	Total    int64 `yaml:"total" json:"total"`
}

type Remote struct {
	UID          string          `json:"uid"`
	Name         string          `json:"name"`
	Home         string          `json:"home"`
	Current      bool            `json:"current"`
	ScriptUID    string          `json:"script_uid"`
	Traffic      ProfileExtra    `json:"traffic"`
	IsJumpSource bool            `json:"is_jump_source,omitempty"`
	IsJumpTarget bool            `json:"is_jump_target,omitempty"`
	MatchRule    *jump.MatchRule `json:"match_rule,omitempty"`
	MatchSummary string          `json:"match_summary,omitempty"`
}

func NewClient(configDir string) Client {
	return Client{
		ConfigDir:   configDir,
		ProfilesDir: filepath.Join(configDir, "profiles"),
	}
}

func DefaultConfigDir() string {
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		return filepath.Join(appdata, "io.github.clash-verge-rev.clash-verge-rev")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "io.github.clash-verge-rev.clash-verge-rev"
	}
	return filepath.Join(home, "AppData", "Roaming", "io.github.clash-verge-rev.clash-verge-rev")
}

func (c Client) ReadProfiles() (ProfilesFile, error) {
	var profiles ProfilesFile
	raw, err := os.ReadFile(filepath.Join(c.ConfigDir, "profiles.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return profiles, nil
		}
		return profiles, err
	}
	if err := yaml.Unmarshal(raw, &profiles); err != nil {
		return ProfilesFile{}, err
	}
	return profiles, nil
}

func (c Client) ProfileItems() ([]ProfileItem, error) {
	profiles, err := c.ReadProfiles()
	if err != nil {
		return nil, err
	}
	return profiles.Items, nil
}

func (c Client) ListRemotes() ([]Remote, error) {
	profiles, err := c.ReadProfiles()
	if err != nil {
		return nil, err
	}
	remotes := []Remote{}
	for _, item := range profiles.Items {
		if item.Type != "remote" {
			continue
		}
		name := item.Name
		if name == "" {
			name = item.UID
		}
		remotes = append(remotes, Remote{
			UID:       item.UID,
			Name:      name,
			Home:      item.Home,
			Current:   item.UID == profiles.Current,
			ScriptUID: item.Option.Script,
			Traffic:   item.Extra,
		})
	}
	return remotes, nil
}

func (c Client) Nodes(subUID string) ([]jump.JumpNode, error) {
	var data struct {
		Proxies []jump.JumpNode `yaml:"proxies"`
	}
	raw, err := os.ReadFile(filepath.Join(c.ProfilesDir, subUID+".yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return []jump.JumpNode{}, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	nodes := []jump.JumpNode{}
	for _, node := range data.Proxies {
		if node.Server != "" {
			nodes = append(nodes, node)
		}
	}
	return nodes, nil
}

func (c Client) FindNode(subUID string, nodeName string) (jump.JumpNode, bool, error) {
	nodes, err := c.Nodes(subUID)
	if err != nil {
		return jump.JumpNode{}, false, err
	}
	for _, node := range nodes {
		if node.Name == nodeName {
			return node, true, nil
		}
	}
	return jump.JumpNode{}, false, nil
}

func (c Client) ScriptUIDForRemote(remoteUID string) (string, bool, error) {
	items, err := c.ProfileItems()
	if err != nil {
		return "", false, err
	}
	for _, item := range items {
		if item.Type == "remote" && item.UID == remoteUID && item.Option.Script != "" {
			return item.Option.Script, true, nil
		}
	}
	return "", false, nil
}

func (c Client) RemoteUIDForScript(scriptUID string) (string, bool, error) {
	items, err := c.ProfileItems()
	if err != nil {
		return "", false, err
	}
	for _, item := range items {
		if item.Type == "remote" && item.Option.Script == scriptUID {
			return item.UID, true, nil
		}
	}
	return "", false, nil
}
