package garmin

import (
	"bufio"
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

// newUTLSClient returns an http.Client that presents the given browser TLS
// fingerprint. No connection pooling: each request dials its own connection.
// roots == nil means system roots.
func newUTLSClient(hello utls.ClientHelloID, roots *x509.CertPool, jar http.CookieJar) *http.Client {
	return &http.Client{Transport: &utlsTransport{hello: hello, roots: roots}, Jar: jar}
}

type utlsTransport struct {
	hello utls.ClientHelloID
	roots *x509.CertPool
}

// closeBody closes the underlying connection together with the response body.
type closeBody struct {
	io.ReadCloser
	closeConn func()
}

func (b closeBody) Close() error {
	err := b.ReadCloser.Close()
	b.closeConn()
	return err
}

func (t *utlsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" {
		return nil, errors.New("utls transport: https only")
	}
	ctx := req.Context()
	port := req.URL.Port()
	if port == "" {
		port = "443"
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(req.URL.Hostname(), port))
	if err != nil {
		return nil, err
	}
	uconn := utls.UClient(conn, &utls.Config{ServerName: req.URL.Hostname(), RootCAs: t.roots}, t.hello)
	if err := uconn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	// Abort in-flight I/O if the context ends before the body is closed.
	stop := context.AfterFunc(ctx, func() { uconn.Close() })
	closeConn := func() { stop(); uconn.Close() }

	if uconn.ConnectionState().NegotiatedProtocol == "h2" {
		cc, err := (&http2.Transport{}).NewClientConn(uconn)
		if err != nil {
			closeConn()
			return nil, err
		}
		resp, err := cc.RoundTrip(req)
		if err != nil {
			closeConn()
			return nil, err
		}
		resp.Body = closeBody{resp.Body, func() { cc.Close(); closeConn() }}
		return resp, nil
	}

	r := req.Clone(ctx)
	r.Close = true
	if err := r.Write(uconn); err != nil {
		closeConn()
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(uconn), req)
	if err != nil {
		closeConn()
		return nil, err
	}
	resp.Body = closeBody{resp.Body, closeConn}
	return resp, nil
}
