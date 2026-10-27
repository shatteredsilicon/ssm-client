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
	GeneratedFiles() []string
	// CustomOptions returns key-value map of custom options that are applied
	CustomOptions() (map[string]string, error)
}

type toolAgent struct {
	baseDir        string
	confItems      []ToolAgentConfItem
	generatedFiles []string
}

type ToolAgentConfItem struct {
	Name       string   `json:"name"`
	Executable string   `json:"executable"`
	Stdin      []string `json:"stdin"`
}

func NewToolAgent(baseDir string, confItems []ToolAgentConfItem, generatedFiles []string) *toolAgent {
	return &toolAgent{
		baseDir:        baseDir,
		confItems:      confItems,
		generatedFiles: generatedFiles,
	}
}

func (ta *toolAgent) Init(ctx context.Context) error {
	toolAgentCfgPath := ta.CfgPath()
	toolAgentCfgFile, err := ini.LoadSources(ini.LoadOptions{
		AllowShadows: true,
		Loose:        true,
	}, toolAgentCfgPath)
	if err != nil {
		return err
	}

	for _, item := range ta.confItems {
		toolAgentCfgFile.Section(item.Name).Key("executable").SetValue(item.Executable)
		for _, stdin := range item.Stdin {
			toolAgentCfgFile.Section(item.Name).NewKey("stdin", stdin)
		}
	}

	var buf bytes.Buffer
	if _, err := toolAgentCfgFile.WriteTo(&buf); err != nil {
		return err
	}

	ta.generatedFiles = append(ta.generatedFiles, toolAgentCfgPath)
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

func (ta *toolAgent) GeneratedFiles() []string {
	return ta.generatedFiles
}
