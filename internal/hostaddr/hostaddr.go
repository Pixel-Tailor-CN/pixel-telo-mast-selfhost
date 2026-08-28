// Package hostaddr 列出本机可用于填写 public_url 的地址。
package hostaddr

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	KindLoopback = "loopback"
	KindLocal    = "local"
	KindPublic   = "public"
)

// Candidate 表示一个可写成 HTTPS 根 URL 的地址。
type Candidate struct {
	URL  string
	IP   string
	Name string
	Kind string
}

var (
	interfaceList   = net.Interfaces
	publicClient    = &http.Client{Timeout: 2 * time.Second}
	publicEndpoints = []string{
		"https://api.ipify.org",
		"https://ifconfig.me/ip",
	}
)

// ListenPort 从 listen 地址取出端口，无法解析时返回 8443。
func ListenPort(listen string) string {
	_, port, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil || port == "" {
		return "8443"
	}
	return port
}

// HTTPSURL 把 IP 或主机名与端口拼成 HTTPS 根 URL。
func HTTPSURL(host, port string) string {
	return "https://" + net.JoinHostPort(host, port)
}

// LocalCandidates 列出当前已启用网卡上的单播地址，含回环，不含链路本地和未指定地址。
func LocalCandidates(port string) []Candidate {
	ifaces, err := interfaceList()
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	var locals []Candidate
	var loopbacks []Candidate
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := ipFromAddr(addr)
			if ip == nil || !usableIP(ip) {
				continue
			}
			key := ip.String()
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			item := Candidate{URL: HTTPSURL(key, port), IP: key, Name: iface.Name}
			if ip.IsLoopback() {
				item.Kind = KindLoopback
				if item.Name == "" {
					item.Name = "loopback"
				}
				loopbacks = append(loopbacks, item)
				continue
			}
			item.Kind = KindLocal
			locals = append(locals, item)
		}
	}
	return append(locals, loopbacks...)
}

// LookupPublicIP 向第三方 HTTPS 服务查询当前出口公网 IP。失败时返回错误，不得把响应写入日志。
func LookupPublicIP(ctx context.Context) (net.IP, error) {
	var last error
	for _, endpoint := range publicEndpoints {
		ip, err := lookupOnePublicIP(ctx, endpoint)
		if err == nil {
			return ip, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("public IP lookup is unavailable")
	}
	return nil, last
}

func lookupOnePublicIP(ctx context.Context, endpoint string) (net.IP, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "pixel-telo-mast-selfhost")
	response, err := publicClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, fmt.Errorf("public IP lookup status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 64))
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(strings.TrimSpace(string(body)))
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return nil, fmt.Errorf("public IP lookup returned an unusable address")
	}
	return ip, nil
}

func ipFromAddr(addr net.Addr) net.IP {
	switch value := addr.(type) {
	case *net.IPNet:
		return value.IP
	case *net.IPAddr:
		return value.IP
	default:
		host, _, err := net.SplitHostPort(addr.String())
		if err == nil {
			return net.ParseIP(host)
		}
		return net.ParseIP(addr.String())
	}
}

func usableIP(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	return ip.To4() != nil || ip.IsGlobalUnicast() || ip.IsLoopback()
}
