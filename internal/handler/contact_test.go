package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/middleware"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/service"
)

type stubContactRepository struct {
	calls int
	err   error
}

func (r *stubContactRepository) Enqueue(context.Context, string, string, string, time.Time, time.Duration, int) error {
	r.calls++
	return r.err
}

func TestContactHandlerSubmit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name, body string
		status     int
		code       string
		repoError  error
	}{
		{"認証なしで受付", `{"category":"request","message":" デモ用の要望です "}`, 202, "", nil},
		{"本文省略", `{"category":"bug"}`, 202, "", nil},
		{"本文null", `{"category":"other","message":null}`, 202, "", nil},
		{"カテゴリなし", `{"message":"要望"}`, 400, "invalid_request", nil},
		{"カテゴリ空", `{"category":""}`, 400, "invalid_request", nil},
		{"カテゴリ不正", `{"category":"unknown"}`, 400, "invalid_request", nil},
		{"カテゴリnull", `{"category":null}`, 400, "invalid_request", nil},
		{"本文の型不正", `{"category":"request","message":5}`, 400, "invalid_request", nil},
		{"JSON不正", `{"category":`, 400, "invalid_request", nil},
		{"本文超過", `{"category":"request","message":"` + strings.Repeat("あ", 2001) + `"}`, 400, "invalid_request", nil},
		{"BodyLimit超過", `{"category":"request","message":"` + strings.Repeat("x", 1<<20) + `"}`, 413, "request_too_large", nil},
		{"DB失敗", `{"category":"request"}`, 500, "internal_error", errors.New("private database error")},
		{"全体上限超過", `{"category":"request"}`, 429, "rate_limited", service.ErrContactLimitExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubContactRepository{err: tt.repoError}
			h := NewContactHandler(service.NewContactService(repo, "admin@example.com"))
			r := gin.New()
			r.Use(middleware.BodyLimit())
			r.POST("/api/v1/contact", h.Submit)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/contact", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tt.code != "" {
				var resp model.ErrorResponse
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatal(err)
				}
				if resp.Error.Code != tt.code || resp.Error.Message == "" {
					t.Fatalf("error=%+v", resp)
				}
				if strings.Contains(w.Body.String(), "private database") {
					t.Fatal("内部エラー詳細が公開された")
				}
				if tt.repoError == nil && repo.calls != 0 {
					t.Fatal("不正な入力でoutboxを呼んだ")
				}
				return
			}
			var resp model.ContactResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			var input model.ContactRequest
			if err := json.Unmarshal([]byte(tt.body), &input); err != nil {
				t.Fatal(err)
			}
			if resp.Category != input.Category || resp.Message != strings.TrimSpace(input.Message) || resp.ReceivedAt.IsZero() || repo.calls != 1 {
				t.Fatalf("response=%+v enqueue calls=%d", resp, repo.calls)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(w.Body.Bytes(), &fields); err != nil {
				t.Fatal(err)
			}
			if len(fields) != 3 || fields["message"] == nil {
				t.Fatal("必須レスポンス項目が不足")
			}
		})
	}
}
