package garmin

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

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

func TestUTLSContextCancel(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		resp, err := newUTLSClient(utls.HelloIOS_Auto, pool, nil).Do(req)
		if err == nil {
			resp.Body.Close()
		}
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("server never received the request")
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request did not return after context cancel")
	}
}
