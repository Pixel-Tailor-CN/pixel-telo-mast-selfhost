package provider

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// 使用虚构号码保留真实 HAR 的卡片结构，不保存浏览器 Cookie 或原始号码。
func sogouLandlineCard(number string) string {
	return `<div class="phone230320"><h3 class="vr-title" vrcid="title.f3a32ab"><a>营销推广-号码查询服务</a></h3><div class="img-flex img-flex-phone"><div class="text-layout"><p class="con">营销推广</p><p class="con">` + number + `</p></div></div><div class="citeurl">电话邦 - dianhua.cn</div></div>`
}

func TestSogouParsesFormattedLandline(t *testing.T) {
	for _, test := range []struct {
		name, number string
		valid        bool
	}{
		{"区号连字符", "0769-12345678", true},
		{"连续号码", "076912345678", true},
		{"另一号码", "0769-12345679", false},
		{"更长号码", "90769-123456789", false},
		{"不同数字块", "0769 12345678", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			label, err := parseSogouCard([]byte(sogouLandlineCard(test.number)), "076912345678")
			if test.valid {
				if err != nil || label != "营销推广" {
					t.Fatalf("label = %q, error = %v", label, err)
				}
			} else if err == nil {
				t.Fatal("mismatched number accepted")
			}
		})
	}
}

func TestSogouAcceptsLargeSearchPageAndKeepsLimit(t *testing.T) {
	card := sogouLandlineCard("0769-12345678")
	pageOverhead := len("<html><!----></html>") + len(card)
	for _, test := range []struct {
		name              string
		padding           int
		compressed, valid bool
	}{
		{"正常搜索页", 350000, false, true},
		{"压缩搜索页", 350000, true, true},
		{"恰好达到上限", 1024*1024 - pageOverhead, false, true},
		{"超限搜索页", 1024 * 1024, false, false},
		{"解压后超限", 1024 * 1024, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := []byte("<html><!--" + strings.Repeat("x", test.padding) + "-->" + card + "</html>")
			headers := make(http.Header)
			if test.compressed {
				var compressed bytes.Buffer
				writer := gzip.NewWriter(&compressed)
				if _, err := writer.Write(body); err != nil {
					t.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				body = compressed.Bytes()
				headers.Set("Content-Encoding", "gzip")
			}
			client := newHTTPClient()
			client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(bytes.NewReader(body))}, nil
			})
			result, err := newSogouProvider(client).Lookup(context.Background(), "076912345678")
			if test.valid {
				if err != nil || result == nil || !result.IsSpam || result.Tag != "营销推广" {
					t.Fatalf("result = %v, error = %v", result, err)
				}
			} else if result != nil || err == nil || !strings.Contains(err.Error(), "exceeds size limit") {
				t.Fatalf("oversized response accepted: %v", err)
			}
		})
	}
}

func TestDefaultProviderResponseLimitUnchanged(t *testing.T) {
	for _, size := range []int{256 * 1024, 256*1024 + 1} {
		client := newHTTPClient()
		client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(strings.Repeat("x", size)))}, nil
		})
		body, _, _, err := doRequest(context.Background(), client, "https://example.com/", nil)
		if size == 256*1024 {
			if err != nil || len(body) != size {
				t.Fatalf("boundary rejected: %v", err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "exceeds size limit") {
			t.Fatalf("default response limit changed: %v", err)
		}
	}
}
