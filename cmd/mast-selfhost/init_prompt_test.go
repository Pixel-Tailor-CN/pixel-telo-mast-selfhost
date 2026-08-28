package main

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/hostaddr"
)

func TestPromptPublicURLSelectsIndex(t *testing.T) {
	candidates := []hostaddr.Candidate{
		{URL: "https://192.168.1.8:8443", IP: "192.168.1.8", Name: "eth0", Kind: hostaddr.KindLocal},
		{URL: "https://127.0.0.1:8443", IP: "127.0.0.1", Name: "lo", Kind: hostaddr.KindLoopback},
	}
	previousIn, previousOut := initStdin, initStdout
	t.Cleanup(func() {
		initStdin = previousIn
		initStdout = previousOut
	})
	initStdin = strings.NewReader("1\n")
	var out bytes.Buffer
	initStdout = &out

	got, err := promptPublicURL(candidates, "8443")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://192.168.1.8:8443" {
		t.Fatalf("url = %q", got)
	}
	if !strings.Contains(out.String(), "网卡 eth0") {
		t.Fatalf("prompt = %s", out.String())
	}
}

func TestPromptPublicURLAcceptsRawIP(t *testing.T) {
	previousIn, previousOut := initStdin, initStdout
	t.Cleanup(func() {
		initStdin = previousIn
		initStdout = previousOut
	})
	initStdin = strings.NewReader("10.0.0.2\n")
	initStdout = ioDiscard()

	got, err := promptPublicURL(nil, "8443")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://10.0.0.2:8443" {
		t.Fatalf("url = %q", got)
	}
}

func TestSelectPublicURLNonInteractiveListsCandidates(t *testing.T) {
	previousInteractive := initInteractive
	previousList := listLocalAddresses
	t.Cleanup(func() {
		initInteractive = previousInteractive
		listLocalAddresses = previousList
	})
	initInteractive = func() bool { return false }
	listLocalAddresses = func(string) []hostaddr.Candidate {
		return []hostaddr.Candidate{{URL: "https://192.168.1.8:8443", IP: "192.168.1.8", Kind: hostaddr.KindLocal}}
	}

	_, err := selectPublicURL("0.0.0.0:8443")
	if err == nil || !strings.Contains(err.Error(), "https://192.168.1.8:8443") || !strings.Contains(err.Error(), "--public-url") {
		t.Fatalf("error = %v", err)
	}
}

func TestSelectPublicURLInteractiveAppendsPublicIP(t *testing.T) {
	previousInteractive := initInteractive
	previousList := listLocalAddresses
	previousLookup := lookupPublicIP
	previousIn, previousOut := initStdin, initStdout
	t.Cleanup(func() {
		initInteractive = previousInteractive
		listLocalAddresses = previousList
		lookupPublicIP = previousLookup
		initStdin = previousIn
		initStdout = previousOut
	})
	initInteractive = func() bool { return true }
	listLocalAddresses = func(string) []hostaddr.Candidate {
		return []hostaddr.Candidate{{URL: "https://192.168.1.8:8443", IP: "192.168.1.8", Kind: hostaddr.KindLocal}}
	}
	lookupPublicIP = func(context.Context) (net.IP, error) { return net.ParseIP("203.0.113.10"), nil }
	initStdin = strings.NewReader("2\n")
	var out bytes.Buffer
	initStdout = &out

	got, err := selectPublicURL("0.0.0.0:8443")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://203.0.113.10:8443" {
		t.Fatalf("url = %q", got)
	}
	if !strings.Contains(out.String(), "公网 IP") {
		t.Fatalf("prompt = %s", out.String())
	}
}

func TestInitDefaultListenInteractivePromptsAndWidensListen(t *testing.T) {
	dir := t.TempDir()
	previousInteractive := initInteractive
	previousList := listLocalAddresses
	previousLookup := lookupPublicIP
	previousIn, previousOut := initStdin, initStdout
	t.Cleanup(func() {
		initInteractive = previousInteractive
		listLocalAddresses = previousList
		lookupPublicIP = previousLookup
		initStdin = previousIn
		initStdout = previousOut
	})
	initInteractive = func() bool { return true }
	listLocalAddresses = func(string) []hostaddr.Candidate {
		return []hostaddr.Candidate{{URL: "https://192.168.1.8:8443", IP: "192.168.1.8", Kind: hostaddr.KindLocal}}
	}
	lookupPublicIP = func(context.Context) (net.IP, error) { return nil, context.Canceled }
	initStdin = strings.NewReader("1\n")
	initStdout = ioDiscard()

	if err := runInit([]string{"--dir", dir}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "listen: \"0.0.0.0:8443\"") || !strings.Contains(got, "public_url: \"https://192.168.1.8:8443\"") {
		t.Fatalf("config = %s", got)
	}
}

func TestInitExplicitLoopbackListenSkipsPrompt(t *testing.T) {
	dir := t.TempDir()
	previous := initInteractive
	t.Cleanup(func() { initInteractive = previous })
	initInteractive = func() bool { return true }
	if err := runInit([]string{"--dir", dir, "--listen", "127.0.0.1:8443"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "listen: \"127.0.0.1:8443\"") || !strings.Contains(got, "public_url: \"https://127.0.0.1:8443\"") {
		t.Fatalf("config = %s", got)
	}
}

func TestInitWildcardListenUsesPromptSelection(t *testing.T) {
	dir := t.TempDir()
	previousInteractive := initInteractive
	previousList := listLocalAddresses
	previousLookup := lookupPublicIP
	previousIn, previousOut := initStdin, initStdout
	t.Cleanup(func() {
		initInteractive = previousInteractive
		listLocalAddresses = previousList
		lookupPublicIP = previousLookup
		initStdin = previousIn
		initStdout = previousOut
	})
	initInteractive = func() bool { return true }
	listLocalAddresses = func(string) []hostaddr.Candidate {
		return []hostaddr.Candidate{{URL: "https://192.168.1.8:8443", IP: "192.168.1.8", Kind: hostaddr.KindLocal}}
	}
	lookupPublicIP = func(context.Context) (net.IP, error) { return nil, context.Canceled }
	initStdin = strings.NewReader("1\n")
	initStdout = ioDiscard()

	if err := runInit([]string{"--dir", dir, "--listen", "0.0.0.0:8443"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "public_url: \"https://192.168.1.8:8443\"") {
		t.Fatalf("config = %s", data)
	}
}

func TestCandidateAccessURLsListsConfiguredAndLocal(t *testing.T) {
	previous := listLocalAddresses
	t.Cleanup(func() { listLocalAddresses = previous })
	listLocalAddresses = func(string) []hostaddr.Candidate {
		return []hostaddr.Candidate{{URL: "https://192.168.1.8:8443"}}
	}
	got := candidateAccessURLs("0.0.0.0:8443", "https://mast.example.com:8443")
	if len(got) < 2 || got[0] != "https://mast.example.com:8443" {
		t.Fatalf("urls = %#v", got)
	}
}

func TestCandidateAccessURLsSkippedForConcreteListen(t *testing.T) {
	if got := candidateAccessURLs("127.0.0.1:8443", "https://127.0.0.1:8443"); len(got) != 0 {
		t.Fatalf("urls = %#v", got)
	}
	if got := candidateAccessURLs("192.168.1.8:8443", "https://192.168.1.8:8443"); len(got) != 0 {
		t.Fatalf("urls = %#v", got)
	}
}

func ioDiscard() *bytes.Buffer { return &bytes.Buffer{} }
