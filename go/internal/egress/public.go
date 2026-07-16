package egress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const maxRedirects = 5

var forbiddenPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

// Resolver and DialContextFunc make the connect-time policy deterministic to test.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

type DialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

type ClientOptions struct {
	Timeout        time.Duration
	AllowedSchemes []string
	Resolver       Resolver
	DialContext    DialContextFunc
}

// NewPublicClient creates a direct HTTP client that validates every redirect and
// pins each connection to an address approved from the same DNS answer set.
func NewPublicClient(opts ClientOptions) *http.Client {
	resolver := opts.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	dial := opts.DialContext
	if dial == nil {
		dialer := &net.Dialer{Timeout: opts.Timeout}
		dial = dialer.DialContext
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = publicDialContext(resolver, dial)

	client := &http.Client{Transport: transport, Timeout: opts.Timeout}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return errors.New("too many redirects")
		}
		return ValidateURL(req.URL, opts.AllowedSchemes)
	}
	return client
}

func ValidateURL(u *url.URL, allowedSchemes []string) error {
	if u == nil || u.Hostname() == "" {
		return errors.New("destination host is required")
	}
	if u.User != nil {
		return errors.New("destination must not include userinfo")
	}
	if u.Fragment != "" {
		return errors.New("destination must not include a fragment")
	}
	allowed := false
	for _, scheme := range allowedSchemes {
		if strings.EqualFold(u.Scheme, scheme) {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("destination scheme %q is not allowed", u.Scheme)
	}
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(u.Hostname()), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".") {
		return errors.New("destination must target a public host")
	}
	if ip, err := netip.ParseAddr(host); err == nil && !IsPublicIP(ip) {
		return errors.New("destination must target a public address")
	}
	return nil
}

func IsPublicIP(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range forbiddenPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

func publicDialContext(resolver Resolver, dial DialContextFunc) DialContextFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("parse destination address: %w", err)
		}
		host = strings.TrimSuffix(host, ".")
		addrs, err := resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve destination %q: %w", host, err)
		}
		if len(addrs) == 0 {
			return nil, fmt.Errorf("destination %q has no addresses", host)
		}
		for _, addr := range addrs {
			if !IsPublicIP(addr) {
				return nil, fmt.Errorf("destination %q resolved to a non-public address", host)
			}
		}
		var lastErr error
		for _, addr := range addrs {
			conn, dialErr := dial(ctx, network, net.JoinHostPort(addr.Unmap().String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		return nil, fmt.Errorf("dial approved destination %q: %w", host, lastErr)
	}
}
