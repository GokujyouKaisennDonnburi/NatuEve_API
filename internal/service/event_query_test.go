package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
)

// stubEventRepository は EventRepository のテスト用スタブ。
type stubEventRepository struct {
	// 呼び出し時に渡された引数を記録する。
	gotSort   string
	gotOrder  string
	gotLimit  int
	gotOffset int
	// ListSummaries / CountSummaries 返却値。
	results    []model.EventSummary
	totalCount int
	err        error
	countErr   error
	// SearchSummaries / CountSearchSummaries 呼び出し時に渡された引数を記録する。
	gotSearchQuery  string
	gotSearchSort   string
	gotSearchOrder  string
	gotSearchLimit  int
	gotSearchOffset int
	gotCountQuery   string
	// SearchSummaries / CountSearchSummaries 返却値。
	searchResults    []model.EventSummary
	searchTotalCount int
	searchErr        error
	searchCountErr   error
	// Create 用: 受け取った引数と返却値。
	gotNewEvent  *model.NewEvent
	createResult model.CreateEventResponse
	createErr    error
	// GetOwnerProfileID 用: 返却値。
	ownerProfileID    string
	ownerProfileIDErr error
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

func (s *stubEventRepository) SearchSummaries(_ context.Context, q, sort, order string, limit, offset int) ([]model.EventSummary, error) {
	s.gotSearchQuery = q
	s.gotSearchSort = sort
	s.gotSearchOrder = order
	s.gotSearchLimit = limit
	s.gotSearchOffset = offset
	return s.searchResults, s.searchErr
}

func (s *stubEventRepository) CountSearchSummaries(_ context.Context, q string) (int, error) {
	s.gotCountQuery = q
	return s.searchTotalCount, s.searchCountErr
}

func (s *stubEventRepository) Create(_ context.Context, e *model.NewEvent) (model.CreateEventResponse, error) {
	s.gotNewEvent = e
	return s.createResult, s.createErr
}

func (s *stubEventRepository) GetOwnerProfileID(_ context.Context, _ string) (string, error) {
	return s.ownerProfileID, s.ownerProfileIDErr
}

func (s *stubEventRepository) GetByID(_ context.Context, _ string) (*model.EventResponse, error) {
	return nil, nil
}

// makeHelper はテストヘルパー共通処理を担う。
func makeHelper(t *testing.T) {
	t.Helper()
}

func TestEventQueryServiceList_Normalization(t *testing.T) {
	t.Helper()

	// ダミーのサマリーデータ（正規化検証には内容不問）。
	dummyResults := []model.EventSummary{
		{ID: "id-1", Title: "テストイベント", EventDate: time.Now().UTC(), CreatedAt: time.Now().UTC()},
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
			svc := NewEventQueryService(stub, "")

			got, err := svc.List(context.Background(), "", tt.inputSort, tt.inputOrder, tt.inputLimit, tt.inputOffset)

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

// TestEventQueryServiceList_Search は q（検索クエリ）の有無による経路切り替えと
// 正規化後の引数が repository に渡ることを検証する。
func TestEventQueryServiceList_Search(t *testing.T) {
	t.Helper()

	searchResults := []model.EventSummary{
		{ID: "id-2", Title: "検索テストイベント", EventDate: time.Now().UTC(), CreatedAt: time.Now().UTC()},
	}
	const searchTotal = 3

	listResults := []model.EventSummary{
		{ID: "id-1", Title: "全件テストイベント", EventDate: time.Now().UTC(), CreatedAt: time.Now().UTC()},
	}
	const listTotal = 42

	tests := []struct {
		name             string
		inputQ           string
		inputSort        string
		inputOrder       string
		inputLimit       int
		inputOffset      int
		searchErr        error
		searchCountErr   error
		wantErr          bool
		wantSearchCalled bool // true: SearchSummaries/CountSearchSummaries 経路、false: ListSummaries/CountSummaries 経路
		wantQ            string
		wantSort         string
		wantOrder        string
		wantLimit        int
		wantOffset       int
		wantTotal        int
	}{
		{
			name:             "正常: qが非空なら SearchSummaries/CountSearchSummaries 経路に入り正規化後の値が渡る",
			inputQ:           "  テント  ",
			inputSort:        "event_date",
			inputOrder:       "asc",
			inputLimit:       10,
			inputOffset:      5,
			wantSearchCalled: true,
			wantQ:            "テント",
			wantSort:         "event_date",
			wantOrder:        "asc",
			wantLimit:        10,
			wantOffset:       5,
			wantTotal:        searchTotal,
		},
		{
			name:             "正常: qが空白のみ(トリム後空文字)なら ListSummaries 経路(全件)に入る",
			inputQ:           "   ",
			inputSort:        "",
			inputOrder:       "",
			inputLimit:       0,
			inputOffset:      0,
			wantSearchCalled: false,
			wantSort:         "created_at",
			wantOrder:        "desc",
			wantLimit:        20,
			wantOffset:       0,
			wantTotal:        listTotal,
		},
		{
			name:             "異常: 検索経路で SearchSummaries のエラーが伝播する",
			inputQ:           "テント",
			inputLimit:       20,
			wantSearchCalled: true,
			searchErr:        errors.New("search db error"),
			wantErr:          true,
		},
		{
			name:             "異常: 検索経路で CountSearchSummaries のエラーが伝播する",
			inputQ:           "テント",
			inputLimit:       20,
			wantSearchCalled: true,
			searchCountErr:   errors.New("count search db error"),
			wantErr:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			makeHelper(t)

			stub := &stubEventRepository{
				results:          listResults,
				totalCount:       listTotal,
				searchResults:    searchResults,
				searchTotalCount: searchTotal,
				searchErr:        tt.searchErr,
				searchCountErr:   tt.searchCountErr,
			}
			svc := NewEventQueryService(stub, "")

			got, err := svc.List(context.Background(), tt.inputQ, tt.inputSort, tt.inputOrder, tt.inputLimit, tt.inputOffset)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("エラーを期待したが nil だった")
				}
				return
			}
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}

			if tt.wantSearchCalled {
				// 検索経路: SearchSummaries/CountSearchSummaries に正規化後の値が渡ること。
				if stub.gotSearchQuery != tt.wantQ {
					t.Errorf("search query: got %q, want %q", stub.gotSearchQuery, tt.wantQ)
				}
				if stub.gotSearchSort != tt.wantSort {
					t.Errorf("search sort: got %q, want %q", stub.gotSearchSort, tt.wantSort)
				}
				if stub.gotSearchOrder != tt.wantOrder {
					t.Errorf("search order: got %q, want %q", stub.gotSearchOrder, tt.wantOrder)
				}
				if stub.gotSearchLimit != tt.wantLimit {
					t.Errorf("search limit: got %d, want %d", stub.gotSearchLimit, tt.wantLimit)
				}
				if stub.gotSearchOffset != tt.wantOffset {
					t.Errorf("search offset: got %d, want %d", stub.gotSearchOffset, tt.wantOffset)
				}
				if stub.gotCountQuery != tt.wantQ {
					t.Errorf("count search query: got %q, want %q", stub.gotCountQuery, tt.wantQ)
				}
				if stub.gotSort != "" {
					t.Errorf("ListSummaries が呼ばれるべきではない: gotSort=%q", stub.gotSort)
				}
			} else {
				// 全件経路: ListSummaries/CountSummaries に正規化後の値が渡ること。
				if stub.gotSort != tt.wantSort {
					t.Errorf("sort: got %q, want %q", stub.gotSort, tt.wantSort)
				}
				if stub.gotOrder != tt.wantOrder {
					t.Errorf("order: got %q, want %q", stub.gotOrder, tt.wantOrder)
				}
				if stub.gotLimit != tt.wantLimit {
					t.Errorf("limit: got %d, want %d", stub.gotLimit, tt.wantLimit)
				}
				if stub.gotOffset != tt.wantOffset {
					t.Errorf("offset: got %d, want %d", stub.gotOffset, tt.wantOffset)
				}
				if stub.gotSearchQuery != "" {
					t.Errorf("SearchSummaries が呼ばれるべきではない: gotSearchQuery=%q", stub.gotSearchQuery)
				}
			}

			if got.TotalCount != tt.wantTotal {
				t.Errorf("totalCount: got %d, want %d", got.TotalCount, tt.wantTotal)
			}
		})
	}
}
