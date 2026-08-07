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
	TokenPath                   string
	RuntimePath                 string
	Listen                      string
	AllowInsecurePrivateNetwork bool
}

func runInit(args []string) error {
	command := newInitCommand()
	command.SetArgs(args)
	return command.Execute()
}

func newInitCommand() *cobra.Command {
	var dir string
	var listen string
	var allowInsecure bool
	var ifMissing bool
	command := &cobra.Command{
		Use:   "init",
		Short: "生成初始配置文件",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if ifMissing {
				if _, err := os.Stat(filepath.Join(dir, "config.yaml")); err == nil {
					return nil
				} else if !os.IsNotExist(err) {
					return err
				}
			}
			return initDataDirectoryWithOptions(dir, listen, allowInsecure)
		},
	}
	command.Flags().StringVar(&dir, "dir", ".", "数据目录")
	command.Flags().StringVar(&listen, "listen", "127.0.0.1:8443", "监听地址")
	command.Flags().BoolVar(&allowInsecure, "allow-insecure-private-network", false, "允许私有网络使用裸 HTTP")
	command.Flags().BoolVar(&ifMissing, "if-missing", false, "配置已存在时直接成功")
	return command
}

func initDataDirectory(dir string) error {
	return initDataDirectoryWithOptions(dir, "127.0.0.1:8443", false)
}

func initDataDirectoryWithOptions(dir, listen string, allowInsecure bool) error {
	data, err := renderExampleConfigWithOptions(dir, listen, allowInsecure)
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
	return renderExampleConfigWithOptions(dir, "127.0.0.1:8443", false)
}

func renderExampleConfigWithOptions(dir, listen string, allowInsecure bool) ([]byte, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve data directory: %w", err)
	}
	tmpl, err := template.New("config.example.yaml").Parse(exampleConfigTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse embedded config template: %w", err)
	}
	values := exampleConfigData{
		TokenPath:                   strconv.Quote(filepath.Join(absDir, "token")),
		RuntimePath:                 strconv.Quote(filepath.Join(absDir, "runtime.db")),
		Listen:                      strconv.Quote(listen),
		AllowInsecurePrivateNetwork: allowInsecure,
	}
	var output bytes.Buffer
	if err := tmpl.Execute(&output, values); err != nil {
		return nil, fmt.Errorf("render embedded config template: %w", err)
	}
	return output.Bytes(), nil
}
