package service

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

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
	gotSearchKeywords []string
	gotSearchSort     string
	gotSearchOrder    string
	gotSearchLimit    int
	gotSearchOffset   int
	gotCountKeywords  []string
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
	// Exists 用: 返却値。
	exists    bool
	existsErr error
	// Cancel 用: 返却値・エラー。
	cancelResult time.Time
	cancelErr    error
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

func (s *stubEventRepository) SearchSummaries(_ context.Context, keywords []string, sort, order string, limit, offset int) ([]model.EventSummary, error) {
	s.gotSearchKeywords = keywords
	s.gotSearchSort = sort
	s.gotSearchOrder = order
	s.gotSearchLimit = limit
	s.gotSearchOffset = offset
	return s.searchResults, s.searchErr
}

func (s *stubEventRepository) CountSearchSummaries(_ context.Context, keywords []string) (int, error) {
	s.gotCountKeywords = keywords
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

func (s *stubEventRepository) Exists(_ context.Context, _ uuid.UUID) (bool, error) {
	return s.exists, s.existsErr
}

func (s *stubEventRepository) Cancel(_ context.Context, _ uuid.UUID) (time.Time, error) {
	return s.cancelResult, s.cancelErr
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

			got, err := svc.List(context.Background(), nil, tt.inputSort, tt.inputOrder, tt.inputLimit, tt.inputOffset)

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

// TestEventQueryServiceList_Search は keywords（AND 検索）の有無による経路切り替えと、
// キーワード正規化（トリム・空要素除去・件数上限）後の値が repository に渡ることを検証する。
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

	// 上限超過ケース用: maxSearchKeywords+2 件を用意し、上限で切り捨てられることを検証する。
	manyInput := make([]string, 0, maxSearchKeywords+2)
	wantMany := make([]string, 0, maxSearchKeywords)
	for i := range maxSearchKeywords + 2 {
		kw := "kw" + strconv.Itoa(i)
		manyInput = append(manyInput, kw)
		if i < maxSearchKeywords {
			wantMany = append(wantMany, kw)
		}
	}

	tests := []struct {
		name             string
		inputKeywords    []string
		inputSort        string
		inputOrder       string
		inputLimit       int
		inputOffset      int
		searchErr        error
		searchCountErr   error
		wantErr          bool
		wantSearchCalled bool // true: SearchSummaries/CountSearchSummaries 経路、false: ListSummaries/CountSummaries 経路
		wantKeywords     []string
		wantSort         string
		wantOrder        string
		wantLimit        int
		wantOffset       int
		wantTotal        int
	}{
		{
			name:             "正常: 複数キーワードで AND 検索経路に入り、トリム後の値が順序どおり渡る",
			inputKeywords:    []string{"  テント  ", "東京"},
			inputSort:        "event_date",
			inputOrder:       "asc",
			inputLimit:       10,
			inputOffset:      5,
			wantSearchCalled: true,
			wantKeywords:     []string{"テント", "東京"},
			wantSort:         "event_date",
			wantOrder:        "asc",
			wantLimit:        10,
			wantOffset:       5,
			wantTotal:        searchTotal,
		},
		{
			name:             "正常: 空要素・空白のみ要素は除去され、残った語で検索経路に入る",
			inputKeywords:    []string{"", "  ", "桜"},
			inputLimit:       20,
			wantSearchCalled: true,
			wantKeywords:     []string{"桜"},
			wantSort:         "created_at",
			wantOrder:        "desc",
			wantLimit:        20,
			wantOffset:       0,
			wantTotal:        searchTotal,
		},
		{
			name:             "正常: 上限(maxSearchKeywords)を超えた分は切り捨てられる",
			inputKeywords:    manyInput,
			inputLimit:       20,
			wantSearchCalled: true,
			wantKeywords:     wantMany,
			wantSort:         "created_at",
			wantOrder:        "desc",
			wantLimit:        20,
			wantOffset:       0,
			wantTotal:        searchTotal,
		},
		{
			name:             "正常: keywords が nil なら ListSummaries 経路(全件)に入る",
			inputKeywords:    nil,
			inputLimit:       0,
			wantSearchCalled: false,
			wantSort:         "created_at",
			wantOrder:        "desc",
			wantLimit:        20,
			wantOffset:       0,
			wantTotal:        listTotal,
		},
		{
			name:             "正常: 全要素が空白のみ(トリム後に有効語なし)なら全件経路に入る",
			inputKeywords:    []string{"  ", ""},
			inputLimit:       0,
			wantSearchCalled: false,
			wantSort:         "created_at",
			wantOrder:        "desc",
			wantLimit:        20,
			wantOffset:       0,
			wantTotal:        listTotal,
		},
		{
			name:             "異常: 検索経路で SearchSummaries のエラーが伝播する",
			inputKeywords:    []string{"テント"},
			inputLimit:       20,
			wantSearchCalled: true,
			searchErr:        errors.New("search db error"),
			wantErr:          true,
		},
		{
			name:             "異常: 検索経路で CountSearchSummaries のエラーが伝播する",
			inputKeywords:    []string{"テント"},
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

			got, err := svc.List(context.Background(), tt.inputKeywords, tt.inputSort, tt.inputOrder, tt.inputLimit, tt.inputOffset)

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
				if !reflect.DeepEqual(stub.gotSearchKeywords, tt.wantKeywords) {
					t.Errorf("search keywords: got %#v, want %#v", stub.gotSearchKeywords, tt.wantKeywords)
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
				if !reflect.DeepEqual(stub.gotCountKeywords, tt.wantKeywords) {
					t.Errorf("count search keywords: got %#v, want %#v", stub.gotCountKeywords, tt.wantKeywords)
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
				if stub.gotSearchKeywords != nil {
					t.Errorf("SearchSummaries が呼ばれるべきではない: gotSearchKeywords=%#v", stub.gotSearchKeywords)
				}
			}

			if got.TotalCount != tt.wantTotal {
				t.Errorf("totalCount: got %d, want %d", got.TotalCount, tt.wantTotal)
			}
		})
	}
}
