package egress

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

type staticResolver map[string][]netip.Addr

func (r staticResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	addrs, ok := r[host]
	if !ok {
		return nil, errors.New("unexpected host")
	}
	return addrs, nil
}

type stubConn struct{}

func (stubConn) Read([]byte) (int, error)         { return 0, errors.New("unused") }
func (stubConn) Write(p []byte) (int, error)      { return len(p), nil }
func (stubConn) Close() error                     { return nil }
func (stubConn) LocalAddr() net.Addr              { return nil }
func (stubConn) RemoteAddr() net.Addr             { return nil }
func (stubConn) SetDeadline(time.Time) error      { return nil }
func (stubConn) SetReadDeadline(time.Time) error  { return nil }
func (stubConn) SetWriteDeadline(time.Time) error { return nil }

func TestPublicClientRejectsForbiddenDNSAnswersBeforeDial(t *testing.T) {
	tests := map[string][]netip.Addr{
		"loopback":      {netip.MustParseAddr("127.0.0.1")},
		"private":       {netip.MustParseAddr("10.0.0.1")},
		"link-local":    {netip.MustParseAddr("169.254.169.254")},
		"carrier-nat":   {netip.MustParseAddr("100.64.0.1")},
		"ipv6-private":  {netip.MustParseAddr("fd00::1")},
		"mapped":        {netip.MustParseAddr("::ffff:127.0.0.1")},
		"nat64-private": {netip.MustParseAddr("64:ff9b::a00:1")},
		"mixed":         {netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.2")},
		"mixed-reverse": {netip.MustParseAddr("10.0.0.2"), netip.MustParseAddr("93.184.216.34")},
	}
	for name, answers := range tests {
		t.Run(name, func(t *testing.T) {
			dialed := false
			client := NewPublicClient(ClientOptions{
				Timeout:        time.Second,
				AllowedSchemes: []string{"https"},
				Resolver:       staticResolver{"safe.example": answers},
				DialContext: func(context.Context, string, string) (net.Conn, error) {
					dialed = true
					return stubConn{}, nil
				},
			})
			dial := client.Transport.(*http.Transport).DialContext
			if _, err := dial(context.Background(), "tcp", "safe.example:443"); err == nil {
				t.Fatal("expected destination policy rejection")
			}
			if dialed {
				t.Fatal("underlying dialer was called for a forbidden DNS answer")
			}
		})
	}
}

func TestPublicClientPinsApprovedDNSAnswer(t *testing.T) {
	var dialed string
	client := NewPublicClient(ClientOptions{
		Timeout:        time.Second,
		AllowedSchemes: []string{"https"},
		Resolver:       staticResolver{"safe.example": {netip.MustParseAddr("93.184.216.34")}},
		DialContext: func(_ context.Context, _ string, address string) (net.Conn, error) {
			dialed = address
			return stubConn{}, nil
		},
	})
	dial := client.Transport.(*http.Transport).DialContext
	conn, err := dial(context.Background(), "tcp", "safe.example:443")
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	_ = conn.Close()
	if dialed != "93.184.216.34:443" {
		t.Fatalf("expected approved address to be pinned, got %q", dialed)
	}
}

func TestPublicClientChecksRedirectDestinations(t *testing.T) {
	client := NewPublicClient(ClientOptions{AllowedSchemes: []string{"https"}})
	for _, raw := range []string{
		"http://public.example/next",
		"https://127.0.0.1/next",
		"https://user@public.example/next",
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.CheckRedirect(&http.Request{URL: u}, nil); err == nil {
			t.Fatalf("expected redirect %q to be rejected", raw)
		}
	}
	u, _ := url.Parse("https://public.example/next")
	if err := client.CheckRedirect(&http.Request{URL: u}, nil); err != nil {
		t.Fatalf("expected public HTTPS redirect compatibility: %v", err)
	}
	via := make([]*http.Request, maxRedirects)
	if err := client.CheckRedirect(&http.Request{URL: u}, via); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("expected redirect limit error, got %v", err)
	}
}
