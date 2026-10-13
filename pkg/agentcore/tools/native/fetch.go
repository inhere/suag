package native

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/inhere/suga/pkg/agentcore/tools"
	"golang.org/x/net/html/charset"

	"github.com/inhere/suga/pkg/agentcore/api"
)

const (
	fetchTimeout         = 20 * time.Second
	fetchMaxDownload     = 5 << 20
	fetchMaxRedirects    = 5
	defaultFetchMaxBytes = 20000
)

// errBlockedAddress marks a connection refused because of its target IP.
var errBlockedAddress = errors.New("blocked address")

// cgnat is 100.64.0.0/10 (carrier-grade NAT), not covered by IsPrivate.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// publicIP reports whether ip may be fetched: not loopback, private,
// link-local (cloud metadata lives there), unspecified, multicast or CGNAT.
func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() && !ip.IsInterfaceLocalMulticast() && !ip.IsMulticast() &&
		!ip.IsUnspecified() && !cgnat.Contains(ip)
}

// WebFetchTool downloads a web page and returns its readable text.
type WebFetchTool struct {
	client *http.Client
	maxOut int // raw result cap in bytes; 0 = DefaultRawOutputBytes
	// checkTarget vets a URL before it is requested (and on every redirect)
	// when a proxy is used; nil without a proxy, where the dial check applies.
	checkTarget func(ctx context.Context, u *url.URL) error
}

// fetchOptions configure newWebFetchTool.
type fetchOptions struct {
	allow func(netip.Addr) bool
	// proxy, when set, receives every request (tools.web_fetch.proxy).
	proxy *url.URL
	// lookup resolves a host name; tests replace it.
	lookup func(ctx context.Context, host string) ([]netip.Addr, error)
}

// NewWebFetchTool builds the tool. Unless allowPrivate is set, private and
// local addresses are refused. Without a proxy every connection is checked at
// dial time against the IP actually connected to, which also covers
// redirects and DNS rebinding; environment proxies are ignored so the check
// sees the real target. With proxy (http, https or socks5 URL) connections go
// only to the proxy, and the target host is resolved and checked before the
// request and on every redirect instead (the proxy may still resolve a name
// differently: a weaker guarantee, accepted to reach sites that need one).
func NewWebFetchTool(allowPrivate bool, proxy string) (*WebFetchTool, error) {
	opts := fetchOptions{allow: publicIP, lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
		return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	}}
	if allowPrivate {
		opts.allow = func(netip.Addr) bool { return true }
	}
	u, err := ParseFetchProxy(proxy)
	if err != nil {
		return nil, err
	}
	opts.proxy = u
	return newWebFetchTool(opts), nil
}

// ParseFetchProxy validates tools.web_fetch.proxy; empty means no proxy.
func ParseFetchProxy(proxy string) (*url.URL, error) {
	if proxy = strings.TrimSpace(proxy); proxy == "" {
		return nil, nil
	}
	u, err := url.Parse(proxy)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5") {
		return nil, fmt.Errorf("tools.web_fetch.proxy must be an http://, https:// or socks5:// URL, got %q", proxy)
	}
	return u, nil
}

func newWebFetchTool(opts fetchOptions) *WebFetchTool {
	allow := opts.allow
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	t := &WebFetchTool{}
	var proxyFunc func(*http.Request) (*url.URL, error)
	if opts.proxy == nil {
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil || !allow(ip) {
				return fmt.Errorf("%w: %s is not a public address", errBlockedAddress, host)
			}
			return nil
		}
	} else {
		proxyFunc = http.ProxyURL(opts.proxy)
		t.checkTarget = func(ctx context.Context, u *url.URL) error {
			return checkHost(ctx, u.Hostname(), allow, opts.lookup)
		}
	}
	transport := &http.Transport{
		Proxy:                 proxyFunc,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: fetchTimeout,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
	}
	t.client = &http.Client{
		Transport: transport,
		Timeout:   fetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= fetchMaxRedirects {
				return fmt.Errorf("stopped after %d redirects", fetchMaxRedirects)
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to unsupported scheme %q", req.URL.Scheme)
			}
			if t.checkTarget != nil {
				return t.checkTarget(req.Context(), req.URL)
			}
			return nil
		},
	}
	return t
}

// checkHost refuses host when it is, or resolves to, an address allow
// rejects. Every resolved address must pass; a failed lookup is refused too.
func checkHost(ctx context.Context, host string, allow func(netip.Addr) bool,
	lookup func(context.Context, string) ([]netip.Addr, error)) error {
	if ip, err := netip.ParseAddr(host); err == nil {
		if !allow(ip) {
			return fmt.Errorf("%w: %s is not a public address", errBlockedAddress, host)
		}
		return nil
	}
	addrs, err := lookup(ctx, host)
	if err != nil || len(addrs) == 0 {
		return fmt.Errorf("%w: cannot resolve %s to check it: %v", errBlockedAddress, host, err)
	}
	for _, ip := range addrs {
		if !allow(ip) {
			return fmt.Errorf("%w: %s resolves to %s, not a public address", errBlockedAddress, host, ip)
		}
	}
	return nil
}

func (t *WebFetchTool) Name() string { return "web_fetch" }
func (t *WebFetchTool) Description() string {
	return "Fetch an http(s) URL and return its readable text (HTML is converted to text keeping headings, " +
		"links and code blocks; scripts and styles are dropped). Private and local network addresses are refused."
}

// ToolMeta marks the tool as a read-only fetch.
func (*WebFetchTool) ToolMeta() tools.Meta {
	return tools.Meta{Category: tools.CategoryFetch, ReadOnly: true, External: true}
}
func (t *WebFetchTool) Schema() api.ToolSchema {
	return api.ToolSchema{
		Name:        t.Name(),
		Description: t.Description(),
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url":       map[string]any{"type": "string", "description": "http or https URL"},
				"max_bytes": map[string]any{"type": "integer", "description": fmt.Sprintf("Maximum bytes of text to return (default %d)", defaultFetchMaxBytes)},
			},
			"required": []any{"url"},
		},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, call api.ToolCall) (*api.ToolResult, error) {
	raw, _ := call.Arguments["url"].(string)
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("web_fetch: url must be an absolute http(s) URL, got %q", raw)
	}
	maxBytes := defaultFetchMaxBytes
	if n, ok := numberArg(call.Arguments["max_bytes"]); ok && n >= 1 {
		maxBytes = int(n)
	}
	maxBytes = min(maxBytes, rawLimit(t.maxOut))

	if t.checkTarget != nil {
		if err := t.checkTarget(ctx, u); err != nil {
			return nil, fmt.Errorf("web_fetch: refused: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("web_fetch: %w", err)
	}
	req.Header.Set("User-Agent", "suga-web-fetch/1.0")
	req.Header.Set("Accept", "text/html,text/plain,application/json;q=0.9,*/*;q=0.5")
	resp, err := t.client.Do(req)
	if err != nil {
		if errors.Is(err, errBlockedAddress) {
			return nil, fmt.Errorf("web_fetch: refused: %w", err)
		}
		return nil, fmt.Errorf("web_fetch: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, fetchMaxDownload+1))
	if err != nil {
		return nil, fmt.Errorf("web_fetch: read body: %w", err)
	}
	clipped := len(body) > fetchMaxDownload
	if clipped {
		body = body[:fetchMaxDownload]
	}
	contentType := resp.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(contentType)

	var header strings.Builder
	fmt.Fprintf(&header, "URL: %s\nStatus: %s\n", resp.Request.URL, resp.Status)
	var text string
	switch {
	case mediaType == "text/html" || mediaType == "application/xhtml+xml" || (mediaType == "" && looksHTML(body)):
		title, content := htmlToText(decodeBody(body, contentType), resp.Request.URL)
		if title != "" {
			fmt.Fprintf(&header, "Title: %s\n", title)
		}
		text = content
	case strings.HasPrefix(mediaType, "text/") || strings.HasSuffix(mediaType, "json") || strings.HasSuffix(mediaType, "xml"):
		text = decodeBody(body, contentType)
	default:
		return &api.ToolResult{CallID: call.CallID, Name: t.Name(),
			Content: header.String() + fmt.Sprintf("[unsupported content type %q, %d bytes; not shown]", mediaType, len(body))}, nil
	}
	if clipped {
		header.WriteString("[download stopped at 5MB]\n")
	}
	content, _, cut := TruncateLines(text, maxBytes)
	if cut {
		content += fmt.Sprintf("\n[truncated at %d bytes of %d]", maxBytes, len(text))
	}
	return &api.ToolResult{CallID: call.CallID, Name: t.Name(), Content: header.String() + "\n" + content}, nil
}

// decodeBody converts body to UTF-8 using the declared or sniffed charset.
func decodeBody(body []byte, contentType string) string {
	r, err := charset.NewReader(strings.NewReader(string(body)), contentType)
	if err != nil {
		return strings.ToValidUTF8(string(body), "�")
	}
	out, err := io.ReadAll(r)
	if err != nil {
		return strings.ToValidUTF8(string(body), "�")
	}
	return string(out)
}

// looksHTML sniffs a body served without a content type.
func looksHTML(body []byte) bool {
	return http.DetectContentType(body) == "text/html; charset=utf-8"
}
