// Package main は、届いたリクエストの接続元と IP 系ヘッダーを JSON で返す、利用者 IP の検証用サービス（#215、ADR-0038）。
//
// API とは別の Cloud Run サービスとしてデプロイし、検証が終わったら削除する。
// 手順は docs/Deployment.md の「利用者 IP の検証（#215）」。
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/middleware"
)

// defaultPort は PORT が未設定のときに待ち受けるポート。
const defaultPort = "8080"

// ipHeaders は値を返すヘッダー。newHandler に渡した clientIPHeader も加わる。
// これ以外のヘッダーは名前だけを返す。
var ipHeaders = []string{
	"X-Forwarded-For",
	"Forwarded",
	"X-Real-IP",
	"True-Client-IP",
	"CF-Connecting-IP",
	"CF-Connecting-IPv6",
	"CF-Pseudo-IPv4",
}

// echoResponse は応答の JSON。ip_headers のキーは http.CanonicalHeaderKey で正規化した名前。
type echoResponse struct {
	RemoteAddr       string              `json:"remote_addr"`
	IPHeaders        map[string][]string `json:"ip_headers"`
	HeaderNames      []string            `json:"header_names"`
	RelayAuthPresent bool                `json:"relay_auth_present"`
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("headerecho exited with error", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	handler, err := newHandler(strings.TrimSpace(os.Getenv("RELAY_CLIENT_IP_HEADER")))
	if err != nil {
		return fmt.Errorf("RELAY_CLIENT_IP_HEADER: %w", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	slog.Info("headerecho listening", slog.String("port", port))
	return srv.ListenAndServe()
}

// newHandler は、どのパス・メソッドでも echoResponse を返すハンドラを返す。
//
// clientIPHeader は中継用 Worker が利用者 IP を入れるヘッダー名（API の RELAY_CLIENT_IP_HEADER と同じ値）。
// 空、または middleware.RelayAuthHeader と同じ名前ならエラーを返す。
// 秘密ヘッダーは有無だけを返し、値は返さない。Host とリクエストの中身はログにも応答にも出さない。
func newHandler(clientIPHeader string) (http.Handler, error) {
	if clientIPHeader == "" {
		return nil, errors.New("no client IP header")
	}
	if strings.EqualFold(clientIPHeader, middleware.RelayAuthHeader) {
		return nil, fmt.Errorf("client IP header must differ from %s", middleware.RelayAuthHeader)
	}

	valueHeaders := make([]string, 0, len(ipHeaders)+1)
	for _, h := range append(slices.Clone(ipHeaders), clientIPHeader) {
		valueHeaders = append(valueHeaders, http.CanonicalHeaderKey(h))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := echoResponse{
			RemoteAddr:       r.RemoteAddr,
			IPHeaders:        make(map[string][]string),
			HeaderNames:      make([]string, 0, len(r.Header)),
			RelayAuthPresent: len(r.Header.Values(middleware.RelayAuthHeader)) > 0,
		}
		for _, h := range valueHeaders {
			if v := r.Header.Values(h); len(v) > 0 {
				resp.IPHeaders[h] = v
			}
		}
		for name := range r.Header {
			resp.HeaderNames = append(resp.HeaderNames, name)
		}
		slices.Sort(resp.HeaderNames)

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			slog.Warn("応答の書き込みに失敗しました", slog.Any("error", err))
		}
	}), nil
}
