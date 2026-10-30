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

func TestUTLS(t *testing.T) {
	for _, h2 := range []bool{false, true} {
		t.Run(fmt.Sprint(h2), func(t *testing.T) {
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/set" {
					http.SetCookie(w, &http.Cookie{Name: "test", Value: "ok"})
				}
				if r.URL.Path == "/get" {
					c, err := r.Cookie("test")
					if err != nil || c.Value != "ok" {
						t.Error("missing cookie")
					}
				}
				fmt.Fprint(w, r.Proto)
			}))
			srv.EnableHTTP2 = h2
			srv.StartTLS()
			defer srv.Close()
			pool := x509.NewCertPool()
			pool.AddCert(srv.Certificate())
			jar, _ := cookiejar.New(nil)
			c := newUTLSClient(utls.HelloChrome_Auto, pool, jar)
			for _, path := range []string{"/set", "/get"} {
				resp, err := c.Get(srv.URL + path)
				if err != nil {
					t.Fatal(err)
				}
				b, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				want := "HTTP/1.1"
				if h2 {
					want = "HTTP/2.0"
				}
				if string(b) != want {
					t.Fatalf("got %s want %s", b, want)
				}
			}
		})
	}
}
