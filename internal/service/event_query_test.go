package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GokujyouKaisennDonnburi/NatuIve_API/internal/model"
)

// stubEventRepository は EventRepository のテスト用スタブ。
type stubEventRepository struct {
	// 呼び出し時に渡された引数を記録する。
	gotSort   string
	gotOrder  string
	gotLimit  int
	gotOffset int
	// 返却値。
	results    []model.EventSummary
	totalCount int
	err        error
	countErr   error
}

func (s *stubEventRepository) ListSummaries(_ context.Context, sort, order string, limit, offset int) ([]model.EventSummary, error) {
	s.gotSort = sort
	s.gotOrder = order
	s.gotLimit = limit
	s.gotOffset = offset
	return s.results, s.err
}

func (s *stubEventRepository) CountSummaries(_ context.Context) (int, error) {
	return s.totalCount, s.countErr
}

// makeHelper はテストヘルパー共通処理を担う。
func makeHelper(t *testing.T) {
	t.Helper()
}

func TestEventQueryServiceList_Normalization(t *testing.T) {
	t.Helper()

	// ダミーのサマリーデータ（正規化検証には内容不問）。
	dummyResults := []model.EventSummary{
		{ID: "id-1", Title: "テストイベント", EventDate: time.Now(), CreatedAt: time.Now()},
	}
	const dummyTotal = 42

	tests := []struct {
		name        string
		inputSort   string
		inputOrder  string
		inputLimit  int
		inputOffset int
		wantSort    string
		wantOrder   string
		wantLimit   int
		wantOffset  int
		repoErr     error
		countErr    error
		wantErr     bool
	}{
		{
			name:        "正常: limit/offset がデフォルト値(0)の場合は default20/0 に正規化",
			inputSort:   "",
			inputOrder:  "",
			inputLimit:  0,
			inputOffset: 0,
			wantSort:    "created_at",
			wantOrder:   "desc",
			wantLimit:   20,
			wantOffset:  0,
		},
		{
			name:        "正常: limit が負値の場合は default20 に正規化",
			inputSort:   "",
			inputOrder:  "",
			inputLimit:  -5,
			inputOffset: 0,
			wantSort:    "created_at",
			wantOrder:   "desc",
			wantLimit:   20,
			wantOffset:  0,
		},
		{
			name:        "正常: limit が 100 超過の場合は 100 に丸める",
			inputSort:   "",
			inputOrder:  "",
			inputLimit:  200,
			inputOffset: 0,
			wantSort:    "created_at",
			wantOrder:   "desc",
			wantLimit:   100,
			wantOffset:  0,
		},
		{
			name:        "正常: limit が最大値ちょうど(100)はそのまま",
			inputSort:   "",
			inputOrder:  "",
			inputLimit:  100,
			inputOffset: 0,
			wantSort:    "created_at",
			wantOrder:   "desc",
			wantLimit:   100,
			wantOffset:  0,
		},
		{
			name:        "正常: limit が有効範囲内はそのまま",
			inputSort:   "",
			inputOrder:  "",
			inputLimit:  50,
			inputOffset: 10,
			wantSort:    "created_at",
			wantOrder:   "desc",
			wantLimit:   50,
			wantOffset:  10,
		},
		{
			name:        "正常: offset が負値の場合は 0 に丸める",
			inputSort:   "",
			inputOrder:  "",
			inputLimit:  20,
			inputOffset: -1,
			wantSort:    "created_at",
			wantOrder:   "desc",
			wantLimit:   20,
			wantOffset:  0,
		},
		{
			name:        "正常: sort=event_date, order=asc はそのまま通る",
			inputSort:   "event_date",
			inputOrder:  "asc",
			inputLimit:  10,
			inputOffset: 0,
			wantSort:    "event_date",
			wantOrder:   "asc",
			wantLimit:   10,
			wantOffset:  0,
		},
		{
			name:        "正常: sort=created_at, order=desc はそのまま通る",
			inputSort:   "created_at",
			inputOrder:  "desc",
			inputLimit:  10,
			inputOffset: 0,
			wantSort:    "created_at",
			wantOrder:   "desc",
			wantLimit:   10,
			wantOffset:  0,
		},
		{
			name:        "正常: sort が不正値の場合は created_at にデフォルト",
			inputSort:   "invalid_column",
			inputOrder:  "desc",
			inputLimit:  10,
			inputOffset: 0,
			wantSort:    "created_at",
			wantOrder:   "desc",
			wantLimit:   10,
			wantOffset:  0,
		},
		{
			name:        "正常: order が不正値の場合は desc にデフォルト",
			inputSort:   "event_date",
			inputOrder:  "invalid_order",
			inputLimit:  10,
			inputOffset: 0,
			wantSort:    "event_date",
			wantOrder:   "desc",
			wantLimit:   10,
			wantOffset:  0,
		},
		{
			name:        "正常: sort・order ともに不正値の場合は両方デフォルト",
			inputSort:   "DROP TABLE events;--",
			inputOrder:  "UNION SELECT",
			inputLimit:  10,
			inputOffset: 0,
			wantSort:    "created_at",
			wantOrder:   "desc",
			wantLimit:   10,
			wantOffset:  0,
		},
		{
			name:        "異常: repository の ListSummaries エラーが伝播する",
			inputSort:   "",
			inputOrder:  "",
			inputLimit:  20,
			inputOffset: 0,
			wantSort:    "created_at",
			wantOrder:   "desc",
			wantLimit:   20,
			wantOffset:  0,
			repoErr:     errors.New("db error"),
			wantErr:     true,
		},
		{
			name:        "異常: repository の CountSummaries エラーが伝播する",
			inputSort:   "",
			inputOrder:  "",
			inputLimit:  20,
			inputOffset: 0,
			wantSort:    "created_at",
			wantOrder:   "desc",
			wantLimit:   20,
			wantOffset:  0,
			countErr:    errors.New("count db error"),
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			makeHelper(t)

			stub := &stubEventRepository{
				results:    dummyResults,
				totalCount: dummyTotal,
				err:        tt.repoErr,
				countErr:   tt.countErr,
			}
			svc := NewEventQueryService(stub)

			got, err := svc.List(context.Background(), tt.inputSort, tt.inputOrder, tt.inputLimit, tt.inputOffset)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("エラーを期待したが nil だった")
				}
				return
			}
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}

			// 正規化後の sort / order が repository に渡されているか確認。
			if stub.gotSort != tt.wantSort {
				t.Errorf("sort: got %q, want %q", stub.gotSort, tt.wantSort)
			}
			if stub.gotOrder != tt.wantOrder {
				t.Errorf("order: got %q, want %q", stub.gotOrder, tt.wantOrder)
			}

			// 正規化後の limit / offset が repository に渡されているか確認。
			if stub.gotLimit != tt.wantLimit {
				t.Errorf("limit: got %d, want %d", stub.gotLimit, tt.wantLimit)
			}
			if stub.gotOffset != tt.wantOffset {
				t.Errorf("offset: got %d, want %d", stub.gotOffset, tt.wantOffset)
			}

			// レスポンスに events・totalCount・limit・offset が正しく入るか確認。
			if len(got.Events) != len(dummyResults) {
				t.Errorf("events 件数: got %d, want %d", len(got.Events), len(dummyResults))
			}
			if got.TotalCount != dummyTotal {
				t.Errorf("totalCount: got %d, want %d", got.TotalCount, dummyTotal)
			}
			if got.Limit != tt.wantLimit {
				t.Errorf("response.Limit: got %d, want %d", got.Limit, tt.wantLimit)
			}
			if got.Offset != tt.wantOffset {
				t.Errorf("response.Offset: got %d, want %d", got.Offset, tt.wantOffset)
			}
		})
	}
}
