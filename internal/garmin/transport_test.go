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

func TestUTLSHTTP2(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, r.Proto)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	resp, err := newUTLSClient(utls.HelloChrome_Auto, pool, nil).Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if got, want := string(body), "HTTP/2.0"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestUTLSHTTP1(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, r.Proto)
	}))
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	resp, err := newUTLSClient(utls.HelloIOS_Auto, pool, nil).Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if got, want := string(body), "HTTP/1.1"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestUTLSCookieJar(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/a" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc", Path: "/"})
			return
		}
		fmt.Fprint(w, r.Header.Get("Cookie"))
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := newUTLSClient(utls.HelloChrome_Auto, pool, jar)

	resp, err := client.Get(srv.URL + "/a")
	if err != nil {
		t.Fatalf("GET /a: %v", err)
	}
	resp.Body.Close()

	resp, err = client.Get(srv.URL + "/b")
	if err != nil {
		t.Fatalf("GET /b: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if got, want := string(body), "session=abc"; got != want {
		t.Fatalf("cookie = %q, want %q", got, want)
	}
}
