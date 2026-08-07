package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"text/template"

	"github.com/spf13/cobra"
)

//go:embed config.example.yaml
var exampleConfigTemplate string

type exampleConfigData struct {
	TokenPath   string
	RuntimePath string
}

func runInit(args []string) error {
	command := newInitCommand()
	command.SetArgs(args)
	return command.Execute()
}

func newInitCommand() *cobra.Command {
	var dir string
	command := &cobra.Command{
		Use:   "init",
		Short: "生成初始配置文件",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return initDataDirectory(dir)
		},
	}
	command.Flags().StringVar(&dir, "dir", ".", "数据目录")
	return command
}

func initDataDirectory(dir string) error {
	data, err := renderExampleConfig(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	configPath := filepath.Join(dir, "config.yaml")
	file, err := os.OpenFile(configPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create config: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = file.Close()
			_ = os.Remove(configPath)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close config: %w", err)
	}
	complete = true
	fmt.Printf("initialized config=%s\n", configPath)
	return nil
}

func renderExampleConfig(dir string) ([]byte, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve data directory: %w", err)
	}
	tmpl, err := template.New("config.example.yaml").Parse(exampleConfigTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse embedded config template: %w", err)
	}
	values := exampleConfigData{
		TokenPath:   strconv.Quote(filepath.Join(absDir, "token")),
		RuntimePath: strconv.Quote(filepath.Join(absDir, "runtime.db")),
	}
	var output bytes.Buffer
	if err := tmpl.Execute(&output, values); err != nil {
		return nil, fmt.Errorf("render embedded config template: %w", err)
	}
	return output.Bytes(), nil
}
