package garmin

import (
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	utls "github.com/refraction-networking/utls"
)

func getBody(t *testing.T, c *http.Client, url string) string {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func poolFor(srv *httptest.Server) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return pool
}

func TestUTLSHTTP2(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, r.Proto) }))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()
	if got := getBody(t, newUTLSClient(utls.HelloChrome_Auto, poolFor(srv), nil), srv.URL); got != "HTTP/2.0" {
		t.Fatalf("proto = %q", got)
	}
}

func TestUTLSHTTP1(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, r.Proto) }))
	defer srv.Close()
	if got := getBody(t, newUTLSClient(utls.HelloIOS_Auto, poolFor(srv), nil), srv.URL); got != "HTTP/1.1" {
		t.Fatalf("proto = %q", got)
	}
}

func TestUTLSCookieJar(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "k", Value: "v", Path: "/"})
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("k")
		if err != nil {
			return
		}
		fmt.Fprint(w, c.Value)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	jar, _ := cookiejar.New(nil)
	c := newUTLSClient(utls.HelloChrome_Auto, poolFor(srv), jar)
	getBody(t, c, srv.URL+"/a")
	if got := getBody(t, c, srv.URL+"/b"); got != "v" {
		t.Fatalf("cookie echo = %q", got)
	}
}
