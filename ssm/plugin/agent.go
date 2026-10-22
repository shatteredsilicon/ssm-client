package plugin

import (
	"bytes"
	"context"
	"os"
	"path"
	"strings"

	"gopkg.in/ini.v1"
)

type Agent interface {
	Init(context.Context) error
	Name() string
	CfgPath() string
	// CustomOptions returns key-value map of custom options that are applied
	CustomOptions() (map[string]string, error)
}

type toolAgent struct {
	baseDir   string
	confItems []ToolAgentConfItem
}

type ToolAgentConfItem struct {
	Name       string `json:"name"`
	Executable string `json:"executable"`
	Output     string `json:"output"`
}

func NewToolAgent(baseDir string, confItems []ToolAgentConfItem) *toolAgent {
	return &toolAgent{
		baseDir:   baseDir,
		confItems: confItems,
	}
}

func (ta *toolAgent) Init(ctx context.Context) error {
	toolAgentCfgPath := ta.CfgPath()
	toolAgentCfgFile, err := ini.LooseLoad(toolAgentCfgPath)
	if err != nil {
		return err
	}

	for _, item := range ta.confItems {
		toolAgentCfgFile.Section(item.Name).Key("executable").SetValue(item.Executable)
		toolAgentCfgFile.Section(item.Name).Key("output").SetValue(item.Output)
	}

	var buf bytes.Buffer
	if _, err := toolAgentCfgFile.WriteTo(&buf); err != nil {
		return err
	}

	return os.WriteFile(toolAgentCfgPath, buf.Bytes(), 0640)
}

func (ta *toolAgent) Name() string {
	return "tool"
}

func (ta *toolAgent) CfgPath() string {
	return path.Join(ta.baseDir, "tool-agent.conf")
}

func (ta *toolAgent) CustomOptions() (map[string]string, error) {
	opts := make(map[string]string)

	cfgFile, err := ini.Load(ta.CfgPath())
	if err != nil {
		return nil, err
	}

	var tools []string
	for _, sectionName := range cfgFile.SectionStrings() {
		if sectionName != ini.DefaultSection && sectionName != "" {
			tools = append(tools, sectionName)
		}
	}

	if len(tools) > 0 {
		opts["tools"] = strings.Join(tools, ",")
	}

	return opts, nil
}
