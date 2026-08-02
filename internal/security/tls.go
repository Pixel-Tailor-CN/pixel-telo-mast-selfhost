package security

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"time"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/config"
)

func PrepareTLS(cfg *config.Config) (*tls.Config, error) {
	if cfg.TLS.Mode == "off" {
		return nil, nil
	}
	certFile, keyFile := cfg.TLS.CertFile, cfg.TLS.KeyFile
	if cfg.TLS.Mode == "auto" {
		if certFile == "" {
			certFile = cfg.Storage.RuntimePath + ".crt"
		}
		if keyFile == "" {
			keyFile = cfg.Storage.RuntimePath + ".key"
		}
		if _, err := os.Stat(certFile); os.IsNotExist(err) {
			if err := generateCertificate(cfg.TLS.PublicURL, certFile, keyFile); err != nil {
				return nil, err
			}
		}
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS certificate: %w", err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("parse TLS certificate: %w", err)
	}
	if time.Now().Before(leaf.NotBefore) || time.Now().After(leaf.NotAfter) {
		return nil, fmt.Errorf("TLS certificate is outside its validity period")
	}
	if cfg.TLS.PublicURL != "" {
		parsed, _ := url.Parse(cfg.TLS.PublicURL)
		if err := leaf.VerifyHostname(parsed.Hostname()); err != nil {
			return nil, fmt.Errorf("TLS certificate SAN mismatch: %w", err)
		}
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}, nil
}

func generateCertificate(publicURL, certFile, keyFile string) error {
	parsed, err := url.Parse(publicURL)
	if err != nil {
		return fmt.Errorf("parse TLS public URL: %w", err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate TLS key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return fmt.Errorf("generate TLS serial: %w", err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: parsed.Hostname()}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(365 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{parsed.Hostname()}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create TLS certificate: %w", err)
	}
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		return fmt.Errorf("write TLS certificate: %w", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		return fmt.Errorf("write TLS key: %w", err)
	}
	return nil
}

func CertificateSPKI(certFile string) (string, error) {
	data, err := os.ReadFile(certFile)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return "", fmt.Errorf("invalid certificate PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return "sha256/" + base64.StdEncoding.EncodeToString(hash[:]), nil
}
