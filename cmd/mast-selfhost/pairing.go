package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/security"
)

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
	publicURL := strings.TrimRight(cfg.TLS.PublicURL, "/")
	if publicURL == "" {
		publicURL = "https://" + cfg.Server.Listen
	}
	pin := ""
	if cfg.TLS.Mode != "off" {
		pin, err = security.CertificateSPKI(cfg.TLS.CertFile)
		if err != nil {
			return err
		}
	}
	fmt.Printf("url=%s token=%s instance_id=%s spki_pin=%s\n", publicURL, string(token), "local", pin)
	return nil
}
