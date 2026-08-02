package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/security"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/storage/runtime"
)

type pairingInfo struct {
	URL        string
	Token      string
	InstanceID string
	SPKIPin    string
}

func runPairing(args []string) error {
	flags := flag.NewFlagSet("pairing", flag.ContinueOnError)
	configPath := flags.String("config", "config.yaml", "config path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
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
	defer repo.Close()
	return repo.EnsureInstanceID(context.Background())
}
