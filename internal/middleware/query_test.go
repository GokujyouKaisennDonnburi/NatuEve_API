package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// newQueryRouter は ValidateQuery 付きのテスト用ルーターを組み立てるヘルパー。
// ハンドラはクエリパラメータ q/limit/offset をそのまま JSON で返す。
func newQueryRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(ValidateQuery())
	r.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"q":      c.Query("q"),
			"limit":  c.Query("limit"),
			"offset": c.Query("offset"),
		})
	})
	return r
}

// doQueryRequest は rawQuery 付きで GET し、レスポンスを返すヘルパー。
func doQueryRequest(t *testing.T, r *gin.Engine, rawQuery string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/test"
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestValidateQueryMiddleware(t *testing.T) {
	tests := []struct {
		name       string
		rawQuery   string
		wantStatus int
	}{
		{
			name:       "クエリなしは200で通過する",
			rawQuery:   "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "正常なクエリは200で通過する",
			rawQuery:   "q=%E8%9D%B6&limit=20&offset=0",
			wantStatus: http.StatusOK,
		},
		{
			// & が9999個で「a=1」区切りが10000個 = パラメータ10000個（上限ちょうど）。
			name:       "パラメータちょうど10000個は200で通過する",
			rawQuery:   strings.Repeat("a=1&", 9999) + "a=1",
			wantStatus: http.StatusOK,
		},
		{
			// & が10000個 = パラメータ10001個（上限超過）。
			name:       "パラメータ10001個は400を返す",
			rawQuery:   strings.Repeat("&", 10000),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "不正なパーセントエンコード(%zz)は400を返す",
			rawQuery:   "q=%zz",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "切り詰められたパーセントエンコード(%E3%8)は400を返す",
			rawQuery:   "q=%E3%8",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "エンコードされていないセミコロンは400を返す",
			rawQuery:   "a=1;b=2",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newQueryRouter(t)
			w := doQueryRequest(t, r, tt.rawQuery)
			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

// TestValidateQueryMiddleware_ValuesReadable は、正常なクエリがミドルウェア通過後も
// ハンドラ側で正しくデコードされて読めることを確認する。
func TestValidateQueryMiddleware_ValuesReadable(t *testing.T) {
	r := newQueryRouter(t)
	w := doQueryRequest(t, r, "q=%E8%9D%B6&limit=20&offset=0")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	for _, want := range []string{`"q":"蝶"`, `"limit":"20"`, `"offset":"0"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body = %q, want contains %q", body, want)
		}
	}
}

// TestValidateQueryMiddleware_ErrorResponseFormat は、400 のレスポンスボディが
// 統一エラーフォーマット（{"error":{"code":"...","message":"..."}}）であることを確認する。
func TestValidateQueryMiddleware_ErrorResponseFormat(t *testing.T) {
	r := newQueryRouter(t)
	w := doQueryRequest(t, r, "q=%zz")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	got := w.Body.String()
	want := `{"error":{"code":"invalid_query","message":"クエリ文字列が不正です"}}`
	if got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}
