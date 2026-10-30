package garmin

import (
	"bufio"
	"crypto/x509"
	"io"
	"net"
	"net/http"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

type utlsTransport struct {
	hello utls.ClientHelloID
	roots *x509.CertPool
}
type closeBody struct {
	io.ReadCloser
	conn net.Conn
}

func (b *closeBody) Close() error {
	err := b.ReadCloser.Close()
	connErr := b.conn.Close()
	if err != nil {
		return err
	}
	return connErr
}
func (t *utlsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" {
		return http.DefaultTransport.RoundTrip(req)
	}
	host := req.URL.Hostname()
	addr := req.URL.Host
	if req.URL.Port() == "" {
		addr = net.JoinHostPort(host, "443")
	}
	conn, err := (&net.Dialer{}).DialContext(req.Context(), "tcp", addr)
	if err != nil {
		return nil, err
	}
	u := utls.UClient(conn, &utls.Config{ServerName: host, RootCAs: t.roots}, t.hello)
	if err = u.HandshakeContext(req.Context()); err != nil {
		conn.Close()
		return nil, err
	}
	if u.ConnectionState().NegotiatedProtocol == "h2" {
		cc, err := (&http2.Transport{}).NewClientConn(u)
		if err != nil {
			conn.Close()
			return nil, err
		}
		resp, err := cc.RoundTrip(req)
		if err != nil {
			conn.Close()
			return nil, err
		}
		resp.Body = &closeBody{resp.Body, conn}
		return resp, nil
	}
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Close = true
	if err = clone.Write(u); err != nil {
		conn.Close()
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(u), clone)
	if err != nil {
		conn.Close()
		return nil, err
	}
	resp.Body = &closeBody{resp.Body, conn}
	return resp, nil
}
func newUTLSClient(hello utls.ClientHelloID, roots *x509.CertPool, jar http.CookieJar) *http.Client {
	return &http.Client{Transport: &utlsTransport{hello: hello, roots: roots}, Jar: jar}
}
