package garmin

import (
	"bufio"
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
	if err := conn.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, err
	}

	if conn.ConnectionState().NegotiatedProtocol == "h2" {
		cc, err := (&http2.Transport{}).NewClientConn(conn)
		if err != nil {
			conn.Close()
			return nil, err
		}
		resp, err := cc.RoundTrip(req)
		if err != nil {
			cc.Close()
			return nil, err
		}
		resp.Body = &connClosingBody{ReadCloser: resp.Body, close: cc.Close}
		return resp, nil
	}

	// HTTP/1.1 over the handshaken conn. Connection: close tells the server to
	// end the stream, which lets the response body reach EOF.
	req = req.Clone(ctx)
	req.Close = true
	if err := req.Write(conn); err != nil {
		conn.Close()
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		conn.Close()
		return nil, err
	}
	resp.Request = req
	resp.Body = &connClosingBody{ReadCloser: resp.Body, close: conn.Close}
	return resp, nil
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
