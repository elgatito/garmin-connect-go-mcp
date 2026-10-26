package garmin

import (
	"bufio"
	"context"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

const utlsDialTimeout = 10 * time.Second

// utlsTransport sends each request over a fresh uTLS connection. SSO login
// makes only a handful of requests, so pooling is not worth the complexity.
type utlsTransport struct {
	hello utls.ClientHelloID
	roots *x509.CertPool
}

// newUTLSClient returns a client that dials with the given TLS fingerprint.
// A nil roots pool means the system roots.
func newUTLSClient(hello utls.ClientHelloID, roots *x509.CertPool, jar http.CookieJar) *http.Client {
	return &http.Client{
		Transport: &utlsTransport{hello: hello, roots: roots},
		Jar:       jar,
	}
}

func (t *utlsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	host := req.URL.Hostname()
	port := req.URL.Port()
	if port == "" {
		port = "443"
	}

	dialer := net.Dialer{Timeout: utlsDialTimeout}
	raw, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		raw.SetDeadline(deadline)
	}

	conn := utls.UClient(raw, &utls.Config{ServerName: host, RootCAs: t.roots}, t.hello)
	// Cancellation without a deadline (and a peer that stalls forever) is not
	// covered by SetDeadline; closing the conn unblocks handshake and reads.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	if err := conn.HandshakeContext(ctx); err != nil {
		stop()
		raw.Close()
		return nil, contextErr(ctx, err)
	}

	if conn.ConnectionState().NegotiatedProtocol == "h2" {
		cc, err := (&http2.Transport{}).NewClientConn(conn)
		if err != nil {
			stop()
			conn.Close()
			return nil, contextErr(ctx, err)
		}
		resp, err := cc.RoundTrip(req)
		if err != nil {
			stop()
			cc.Close()
			return nil, contextErr(ctx, err)
		}
		resp.Body = &connClosingBody{ReadCloser: resp.Body, close: func() error {
			stop()
			return cc.Close()
		}}
		return resp, nil
	}

	// HTTP/1.1 over the handshaken conn. Connection: close tells the server to
	// end the stream, which lets the response body reach EOF.
	req = req.Clone(ctx)
	req.Close = true
	if err := req.Write(conn); err != nil {
		stop()
		conn.Close()
		return nil, contextErr(ctx, err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		stop()
		conn.Close()
		return nil, contextErr(ctx, err)
	}
	resp.Request = req
	resp.Body = &connClosingBody{ReadCloser: resp.Body, close: func() error {
		stop()
		return conn.Close()
	}}
	return resp, nil
}

// contextErr reports the context error when cancellation closed the conn.
func contextErr(ctx context.Context, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	return err
}

// connClosingBody closes the connection when the response body is closed.
type connClosingBody struct {
	io.ReadCloser
	close func() error
}

func (b *connClosingBody) Close() error {
	err := b.ReadCloser.Close()
	if cerr := b.close(); err == nil {
		err = cerr
	}
	return err
}
