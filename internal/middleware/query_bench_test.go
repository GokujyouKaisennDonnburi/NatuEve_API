package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// buildMaxRealisticQuery は ADR-0021 が想定する「現実的な最大構成」のクエリ文字列を組み立てる。
// tagId 20件（UUID）+ q（日本語10語をパーセントエンコード）+ sort/order/limit/offset で
// 約1.4KBになる。
func buildMaxRealisticQuery() string {
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		if sb.Len() > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString("tagId=")
		sb.WriteString(uuid.New().String())
	}

	words := []string{
		"オオムラサキ", "アサギマダラ", "ミヤマクワガタ", "ノコギリクワガタ", "アオスジアゲハ",
		"ジャコウアゲハ", "ギンヤンマ", "オニヤンマ", "ナナホシテントウ", "カブトムシ",
	}
	sb.WriteString("&q=")
	sb.WriteString(url.QueryEscape(strings.Join(words, " ")))

	sb.WriteString("&sort=name&order=asc&limit=20&offset=0")
	return sb.String()
}

// runQueryBenchmark は指定したハンドラを rawQuery 付きで繰り返し呼び出す共通処理。
//
// AbortWithStatusJSON がレスポンスへの書き込みで状態を持つため、gin.Context と
// httptest.ResponseRecorder はイテレーションごとに作り直す(*http.Request は
// ValidateQuery からは読み取り専用のため使い回してよい)。
//
// 注意: このハーネス自体（ResponseRecorder と gin.Context の生成）のコストが
// 数百 ns 単位で乗るため、各ベンチの絶対値をそのまま「ValidateQuery のコスト」と
// 読んではならない。BenchmarkBaseline_NoMiddleware との差分が実コストである。
func runQueryBenchmark(b *testing.B, handler gin.HandlerFunc, rawQuery string) {
	b.Helper()
	gin.SetMode(gin.TestMode)

	target := "/test"
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)

	b.ReportAllocs()
	for b.Loop() {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		handler(c)
	}
}

// runValidateQueryBenchmark は ValidateQuery() を計測する。
func runValidateQueryBenchmark(b *testing.B, rawQuery string) {
	b.Helper()
	runQueryBenchmark(b, ValidateQuery(), rawQuery)
}

// BenchmarkBaseline_NoMiddleware は計測ハーネス自体のコストを測る基準値。
// 何もしないハンドラを同じ手順で呼ぶため、この値が ValidateQuery を入れなかった
// 場合のコストに相当する。各 BenchmarkValidateQuery_* からこの値を引いた差が
// ミドルウェア追加による実際の増分になる。
func BenchmarkBaseline_NoMiddleware(b *testing.B) {
	runQueryBenchmark(b, func(c *gin.Context) { c.Next() }, "")
}

// BenchmarkValidateQuery_NoQuery はクエリなし（RawQuery 空）のショートサーキットを計測する。
func BenchmarkValidateQuery_NoQuery(b *testing.B) {
	runValidateQueryBenchmark(b, "")
}

// BenchmarkValidateQuery_Typical は典型的なクエリを計測する。
func BenchmarkValidateQuery_Typical(b *testing.B) {
	runValidateQueryBenchmark(b, "q=%E8%9D%B6&limit=20&offset=0")
}

// BenchmarkValidateQuery_MaxRealistic は現実的な最大構成（約1.4KB）のクエリを計測する。
func BenchmarkValidateQuery_MaxRealistic(b *testing.B) {
	raw := buildMaxRealisticQuery()
	runValidateQueryBenchmark(b, raw)
}

// BenchmarkValidateQuery_OverLimit はパラメータ数が上限(10,000)を超えるクエリを計測する。
func BenchmarkValidateQuery_OverLimit(b *testing.B) {
	runValidateQueryBenchmark(b, strings.Repeat("&", 10000))
}
