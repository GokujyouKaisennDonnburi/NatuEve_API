package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/middleware"
)

const (
	testClientIPHeader = "X-NatuEve-Client-IP"
	testRemoteAddr     = "169.254.1.1:43210"
	testRelaySecret    = "relay-secret-0123456789abcdef0123456789"
)

func TestNewHandlerValidation(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		wantErr bool
	}{
		{name: "ヘッダー名あり", header: testClientIPHeader},
		{name: "IP 系のヘッダーと同じ名前", header: "CF-Connecting-IP"},
		{name: "空", header: "", wantErr: true},
		{name: "秘密ヘッダーと同じ名前", header: middleware.RelayAuthHeader, wantErr: true},
		{name: "秘密ヘッダーの大文字小文字違い", header: "x-relay-auth", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newHandler(tt.header)
			if (err != nil) != tt.wantErr {
				t.Errorf("newHandler(%q) error = %v, wantErr %v", tt.header, err, tt.wantErr)
			}
		})
	}
}

func TestHandler(t *testing.T) {
	tests := []struct {
		name string
		// clientIPHeader は newHandler に渡すヘッダー名。空なら testClientIPHeader。
		clientIPHeader string
		method         string
		path           string
		headers        [][2]string // 送る順に並べる。同じ名前を複数回送れる
		want           echoResponse
		// mustNotContain は応答の本文に含まれてはならない文字列。
		mustNotContain []string
	}{
		{
			name:   "IP 系のヘッダーと利用者 IP のヘッダーは値を返す",
			method: http.MethodGet,
			path:   "/",
			headers: [][2]string{
				{"X-Forwarded-For", "203.0.113.1, 198.51.100.2"},
				{"Forwarded", "for=203.0.113.1"},
				{"CF-Connecting-IP", "2001:db8::1"},
				{"CF-Pseudo-IPv4", "240.0.0.1"},
				{testClientIPHeader, "2001:db8::1"},
			},
			want: echoResponse{
				RemoteAddr: testRemoteAddr,
				IPHeaders: map[string][]string{
					"X-Forwarded-For":     {"203.0.113.1, 198.51.100.2"},
					"Forwarded":           {"for=203.0.113.1"},
					"Cf-Connecting-Ip":    {"2001:db8::1"},
					"Cf-Pseudo-Ipv4":      {"240.0.0.1"},
					"X-Natueve-Client-Ip": {"2001:db8::1"},
				},
				HeaderNames: []string{"Cf-Connecting-Ip", "Cf-Pseudo-Ipv4", "Forwarded", "X-Forwarded-For", "X-Natueve-Client-Ip"},
			},
		},
		{
			name:   "同じヘッダーが複数あればすべての値を返す",
			method: http.MethodPost,
			path:   "/api/v1/events/x/join",
			headers: [][2]string{
				{"X-Forwarded-For", "203.0.113.1"},
				{"X-Forwarded-For", "198.51.100.2"},
			},
			want: echoResponse{
				RemoteAddr:  testRemoteAddr,
				IPHeaders:   map[string][]string{"X-Forwarded-For": {"203.0.113.1", "198.51.100.2"}},
				HeaderNames: []string{"X-Forwarded-For"},
			},
		},
		{
			name:   "秘密ヘッダーと IP 系でないヘッダーは名前だけを返す",
			method: http.MethodGet,
			path:   "/health",
			headers: [][2]string{
				{middleware.RelayAuthHeader, testRelaySecret},
				{"Authorization", "Bearer secret-token"},
				{"Cookie", "session=secret-cookie"},
				{"X-Real-IP", "203.0.113.9"},
			},
			want: echoResponse{
				RemoteAddr:       testRemoteAddr,
				IPHeaders:        map[string][]string{"X-Real-Ip": {"203.0.113.9"}},
				HeaderNames:      []string{"Authorization", "Cookie", "X-Real-Ip", "X-Relay-Auth"},
				RelayAuthPresent: true,
			},
			mustNotContain: []string{testRelaySecret, "secret-token", "secret-cookie"},
		},
		{
			name:           "小文字で指定した利用者 IP のヘッダーも値を返す",
			clientIPHeader: "x-natueve-client-ip",
			method:         http.MethodGet,
			path:           "/",
			headers:        [][2]string{{testClientIPHeader, "203.0.113.5"}},
			want: echoResponse{
				RemoteAddr:  testRemoteAddr,
				IPHeaders:   map[string][]string{"X-Natueve-Client-Ip": {"203.0.113.5"}},
				HeaderNames: []string{"X-Natueve-Client-Ip"},
			},
		},
		{
			name:   "ヘッダーがなければ空で返す",
			method: http.MethodGet,
			path:   "/",
			want: echoResponse{
				RemoteAddr:  testRemoteAddr,
				IPHeaders:   map[string][]string{},
				HeaderNames: []string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientIPHeader := tt.clientIPHeader
			if clientIPHeader == "" {
				clientIPHeader = testClientIPHeader
			}
			handler, err := newHandler(clientIPHeader)
			if err != nil {
				t.Fatalf("newHandler(%q) returned error: %v", clientIPHeader, err)
			}

			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.RemoteAddr = testRemoteAddr
			for _, h := range tt.headers {
				req.Header.Add(h[0], h[1])
			}

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
			}
			if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}

			body := w.Body.String()
			for _, s := range tt.mustNotContain {
				if strings.Contains(body, s) {
					t.Errorf("body contains %q: %s", s, body)
				}
			}

			var got echoResponse
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("failed to parse body %q: %v", body, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("response = %+v, want %+v", got, tt.want)
			}
		})
	}
}
