package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/Pixel-Tailor-CN/pixel-telo-mast-selfhost/internal/hostaddr"
)

var (
	initStdin          io.Reader = os.Stdin
	initStdout         io.Writer = os.Stdout
	initInteractive              = stdinIsInteractive
	listLocalAddresses           = hostaddr.LocalCandidates
	lookupPublicIP               = hostaddr.LookupPublicIP
)

func stdinIsInteractive() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func selectPublicURL(listen string) (string, error) {
	port := hostaddr.ListenPort(listen)
	candidates := listLocalAddresses(port)
	if initInteractive() {
		if publicIP, err := lookupPublicIP(context.Background()); err == nil {
			candidates = append(candidates, hostaddr.Candidate{
				URL:  hostaddr.HTTPSURL(publicIP.String(), port),
				IP:   publicIP.String(),
				Name: "api.ipify.org / ifconfig.me",
				Kind: hostaddr.KindPublic,
			})
		}
		return promptPublicURL(candidates, port)
	}
	return "", fmt.Errorf("%s", nonInteractivePublicURLError(listen, candidates))
}

func promptPublicURL(candidates []hostaddr.Candidate, port string) (string, error) {
	if _, err := fmt.Fprintln(initStdout, "未指定 --public-url。请选择手机或客户端实际访问的地址："); err != nil {
		return "", err
	}
	if runningInContainer() {
		if _, err := fmt.Fprintln(initStdout, "当前像是在容器里，下面的地址多半是容器网卡，手机通常连不上。请输入宿主机局域网 IP 对应的 https://IP:端口。"); err != nil {
			return "", err
		}
	}
	for index, item := range candidates {
		if _, err := fmt.Fprintf(initStdout, "  %d) %s  %s\n", index+1, item.URL, candidateLabel(item)); err != nil {
			return "", err
		}
	}
	if _, err := fmt.Fprintln(initStdout, "输入序号，或粘贴完整 https:// 地址："); err != nil {
		return "", err
	}

	reader := bufio.NewReader(initStdin)
	for {
		if _, err := fmt.Fprint(initStdout, "> "); err != nil {
			return "", err
		}
		line, err := reader.ReadString('\n')
		if err != nil && len(strings.TrimSpace(line)) == 0 {
			return "", fmt.Errorf("read public URL selection: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			if _, err := fmt.Fprintln(initStdout, "请输入序号或 https:// 地址"); err != nil {
				return "", err
			}
			continue
		}
		if selected, err := strconv.Atoi(line); err == nil {
			if selected < 1 || selected > len(candidates) {
				if _, err := fmt.Fprintln(initStdout, "序号超出范围"); err != nil {
					return "", err
				}
				continue
			}
			item := candidates[selected-1]
			if item.Kind == hostaddr.KindPublic {
				if _, err := fmt.Fprintln(initStdout, "已选择公网 IP。若要从外网访问，还需要在路由器做端口转发。"); err != nil {
					return "", err
				}
			}
			return item.URL, nil
		}
		if !strings.Contains(line, "://") {
			if ip := net.ParseIP(line); ip != nil {
				line = hostaddr.HTTPSURL(ip.String(), port)
			} else {
				line = "https://" + line
			}
		}
		if err := validateInitPublicURL(line); err != nil {
			if _, err := fmt.Fprintln(initStdout, "地址无效，需要类似 https://192.168.1.8:8443"); err != nil {
				return "", err
			}
			continue
		}
		return line, nil
	}
}

func nonInteractivePublicURLError(listen string, candidates []hostaddr.Candidate) string {
	var builder strings.Builder
	builder.WriteString("tls public URL is required when listen is not a concrete host")
	if runningInContainer() {
		builder.WriteString("; this process looks like it is running in a container, so listed addresses are usually not reachable from a phone")
	}
	if len(candidates) > 0 {
		builder.WriteString("; detected candidates:")
		for _, item := range candidates {
			builder.WriteString(" ")
			builder.WriteString(item.URL)
		}
	}
	builder.WriteString("; pass --public-url with the HTTPS root URL the client can reach, for example --public-url https://192.168.1.8:")
	builder.WriteString(hostaddr.ListenPort(listen))
	return builder.String()
}

func candidateLabel(item hostaddr.Candidate) string {
	switch item.Kind {
	case hostaddr.KindLoopback:
		return "仅本机 (" + item.Name + ")"
	case hostaddr.KindPublic:
		return "公网 IP（向第三方查询，需确认后再用）"
	default:
		if item.Name == "" {
			return "本机网卡"
		}
		return "网卡 " + item.Name
	}
}

func runningInContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	return false
}

func listenIsUnspecified(listen string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil {
		return false
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return true
	}
	return false
}

func candidateAccessURLs(listen, configured string) []string {
	if !listenIsUnspecified(listen) {
		return nil
	}
	port := hostaddr.ListenPort(listen)
	seen := make(map[string]struct{})
	var urls []string
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if _, exists := seen[raw]; exists {
			return
		}
		seen[raw] = struct{}{}
		urls = append(urls, raw)
	}
	add(strings.TrimRight(configured, "/"))
	if inferred, ok := inferredPublicURL(listen); ok {
		add(inferred)
	}
	for _, item := range listLocalAddresses(port) {
		add(item.URL)
	}
	return urls
}
