package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/security"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/storage/runtime"
	"github.com/spf13/cobra"
)

type pairingInfo struct {
	URL        string
	Token      string
	InstanceID string
	SPKIPin    string
}

func runPairing(args []string) error {
	command := newPairingCommand()
	command.SetArgs(args)
	return command.Execute()
}

func newPairingCommand() *cobra.Command {
	var configPath string
	command := &cobra.Command{
		Use:   "pairing",
		Short: "输出客户端配对信息",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return printPairingInfo(configPath)
		},
	}
	command.Flags().StringVar(&configPath, "config", "config.yaml", "配置文件路径")
	return command
}

func printPairingInfo(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	token, err := config.ReadToken(cfg.Auth.TokenFile)
	if err != nil {
		return err
	}
	info, err := buildPairingInfo(cfg, token)
	if err != nil {
		return err
	}
	fmt.Printf("url=%s token=%s instance_id=%s spki_pin=%s\n", info.URL, info.Token, info.InstanceID, info.SPKIPin)
	return nil
}

func buildPairingInfo(cfg *config.Config, token []byte) (pairingInfo, error) {
	instanceID, err := instanceID(cfg)
	if err != nil {
		return pairingInfo{}, err
	}
	publicURL := strings.TrimRight(cfg.TLS.PublicURL, "/")
	if publicURL == "" {
		publicURL = "https://" + cfg.Server.Listen
	}
	pin := ""
	if cfg.TLS.Mode != "off" {
		certFile, _ := cfg.TLSFiles()
		pin, err = security.CertificateSPKI(certFile)
		if err != nil {
			return pairingInfo{}, err
		}
	}
	return pairingInfo{URL: publicURL, Token: string(token), InstanceID: instanceID, SPKIPin: pin}, nil
}

func instanceID(cfg *config.Config) (string, error) {
	repo, err := runtime.Open(cfg.Storage.RuntimePath)
	if err != nil {
		return "", err
	}
	value, ensureErr := repo.EnsureInstanceID(context.Background())
	closeErr := repo.Close()
	if ensureErr != nil {
		return "", ensureErr
	}
	if closeErr != nil {
		return "", fmt.Errorf("close runtime database: %w", closeErr)
	}
	return value, nil
}
