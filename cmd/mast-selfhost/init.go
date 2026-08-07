package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	ProviderIDs                 string
	SyncOnStart                 bool
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
	var providerIDs []string
	syncOnStart := true
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
			return initDataDirectoryWithProviders(dir, listen, allowInsecure, providerIDs, syncOnStart)
		},
	}
	command.Flags().StringVar(&dir, "dir", ".", "数据目录")
	command.Flags().StringVar(&listen, "listen", "127.0.0.1:8443", "监听地址")
	command.Flags().BoolVar(&allowInsecure, "allow-insecure-private-network", false, "允许私有网络使用裸 HTTP")
	command.Flags().BoolVar(&ifMissing, "if-missing", false, "配置已存在时直接成功")
	command.Flags().StringSliceVar(&providerIDs, "provider-id", nil, "显式启用的 Provider，可重复传入")
	command.Flags().BoolVar(&syncOnStart, "sync-on-start", true, "启动时是否同步 baseline")
	return command
}

func initDataDirectory(dir string) error {
	return initDataDirectoryWithOptions(dir, "127.0.0.1:8443", false)
}

func initDataDirectoryWithOptions(dir, listen string, allowInsecure bool) error {
	return initDataDirectoryWithProviders(dir, listen, allowInsecure, nil, true)
}

func initDataDirectoryWithProviders(dir, listen string, allowInsecure bool, providerIDs []string, syncOnStart bool) error {
	data, err := renderExampleConfigWithProviders(dir, listen, allowInsecure, providerIDs, syncOnStart)
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
	return renderExampleConfigWithProviders(dir, listen, allowInsecure, nil, true)
}

func renderExampleConfigWithProviders(dir, listen string, allowInsecure bool, providerIDs []string, syncOnStart bool) ([]byte, error) {
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
		ProviderIDs:                 providerIDsYAML(providerIDs),
		SyncOnStart:                 syncOnStart,
	}
	var output bytes.Buffer
	if err := tmpl.Execute(&output, values); err != nil {
		return nil, fmt.Errorf("render embedded config template: %w", err)
	}
	return output.Bytes(), nil
}

func providerIDsYAML(providerIDs []string) string {
	if len(providerIDs) == 0 {
		return "[]"
	}
	quoted := make([]string, 0, len(providerIDs))
	for _, providerID := range providerIDs {
		quoted = append(quoted, strconv.Quote(strings.TrimSpace(providerID)))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
