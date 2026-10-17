package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// stubOutboxProcessor は OutboxProcessor のテスト用スタブ。
type stubOutboxProcessor struct {
	processed int
	err       error
	calls     int
	// gotCtxErr は ProcessOnce に渡された ctx の、呼び出し時点の Err() を記録する。
	gotCtxErr error
}

func (s *stubOutboxProcessor) ProcessOnce(ctx context.Context) (int, error) {
	s.calls++
	s.gotCtxErr = ctx.Err()
	return s.processed, s.err
}

func TestOutboxHandler_Process(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		processor     *stubOutboxProcessor
		wantStatus    int
		wantProcessed *float64
		wantErrCode   string
	}{
		{
			name:          "正常: 処理した件数を返す",
			processor:     &stubOutboxProcessor{processed: 3},
			wantStatus:    http.StatusOK,
			wantProcessed: ptr(3.0),
		},
		{
			name:          "正常: 対象が 0 件でも 200 で 0 を返す",
			processor:     &stubOutboxProcessor{},
			wantStatus:    http.StatusOK,
			wantProcessed: ptr(0.0),
		},
		{
			name:        "異常: 処理に失敗したら 500 を統一フォーマットで返す",
			processor:   &stubOutboxProcessor{err: errors.New("db down")},
			wantStatus:  http.StatusInternalServerError,
			wantErrCode: "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.POST("/internal/outbox/process", NewOutboxHandler(context.Background(), tt.processor).Process)

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/internal/outbox/process", nil))

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", w.Code, tt.wantStatus, w.Body.String())
			}
			if tt.processor.calls != 1 {
				t.Errorf("ProcessOnce 呼び出し回数 = %d, want 1", tt.processor.calls)
			}

			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("レスポンスを解析できない %q: %v", w.Body.String(), err)
			}
			if tt.wantProcessed != nil && body["processed"] != *tt.wantProcessed {
				t.Errorf("processed = %v, want %v", body["processed"], *tt.wantProcessed)
			}
			if tt.wantErrCode != "" {
				errBody, _ := body["error"].(map[string]any)
				if errBody["code"] != tt.wantErrCode {
					t.Errorf("error.code = %v, want %q", errBody["code"], tt.wantErrCode)
				}
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func TestOutboxHandler_Process_StopCancelsProcessing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		stopped    bool
		wantCtxErr bool
	}{
		{name: "stop が生きていれば ctx はキャンセルされていない", stopped: false, wantCtxErr: false},
		{name: "stop がキャンセル済みなら ProcessOnce の ctx もキャンセルされる", stopped: true, wantCtxErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stop, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.stopped {
				cancel()
			}

			processor := &stubOutboxProcessor{}
			r := gin.New()
			r.POST("/internal/outbox/process", NewOutboxHandler(stop, processor).Process)

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/internal/outbox/process", nil))

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
			}
			if gotErr := processor.gotCtxErr != nil; gotErr != tt.wantCtxErr {
				t.Errorf("ProcessOnce の ctx.Err() = %v, want キャンセル済み=%v", processor.gotCtxErr, tt.wantCtxErr)
			}
		})
	}
}
