package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
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
	gotSearchFilter model.EventSearchFilter
	gotSearchSort   string
	gotSearchOrder  string
	gotSearchLimit  int
	gotSearchOffset int
	gotCountFilter  model.EventSearchFilter
	// SearchSummaries / CountSearchSummaries 返却値。
	searchResults    []model.EventSummary
	searchTotalCount int
	searchErr        error
	searchCountErr   error
	// ListMySummaries 呼び出し時に渡された引数を記録する。
	gotMyFilter model.MyEventFilter
	gotMySort   string
	gotMyOrder  string
	gotMyLimit  int
	gotMyOffset int
	// CountMyEventKinds 呼び出し時に渡された引数を記録する。
	gotMyCountProfileID uuid.UUID
	// ListMySummaries / CountMyEventKinds 返却値。
	myResults   []model.EventSummary
	myErr       error
	myCounts    model.MyEventCounts
	myCountsErr error
	// 各メソッドが実際に呼ばれたかどうかの記録（経路切り替え・未呼び出しの検証に使う）。
	listCalled        bool
	countCalled       bool
	searchCalled      bool
	countSearchCalled bool
	listMyCalled      bool
	countMyCalled     bool
	// Create 用: 受け取った引数と返却値。
	gotNewEvent  *model.NewEvent
	createResult model.CreateEventResponse
	createErr    error
	// GetOwnerProfileID 用: 返却値。
	ownerProfileID    string
	ownerProfileIDErr error
	// GetTitle 用: 返却値・呼び出し有無の記録。
	title          string
	titleErr       error
	getTitleCalled bool
	// Exists 用: 返却値。
	exists    bool
	existsErr error
	// CancelWithNotification 用: 返却値・エラー・受け取った引数の記録。
	cancelResult     time.Time
	cancelErr        error
	gotCancelSubject string
	gotCancelBody    string
}

func (s *stubEventRepository) ListSummaries(_ context.Context, sort, order string, limit, offset int) ([]model.EventSummary, error) {
	s.listCalled = true
	s.gotSort = sort
	s.gotOrder = order
	s.gotLimit = limit
	s.gotOffset = offset
	return s.results, s.err
}

func (s *stubEventRepository) CountSummaries(_ context.Context) (int, error) {
	s.countCalled = true
	return s.totalCount, s.countErr
}

func (s *stubEventRepository) SearchSummaries(_ context.Context, filter model.EventSearchFilter, sort, order string, limit, offset int) ([]model.EventSummary, error) {
	s.searchCalled = true
	s.gotSearchFilter = filter
	s.gotSearchSort = sort
	s.gotSearchOrder = order
	s.gotSearchLimit = limit
	s.gotSearchOffset = offset
	return s.searchResults, s.searchErr
}

func (s *stubEventRepository) CountSearchSummaries(_ context.Context, filter model.EventSearchFilter) (int, error) {
	s.countSearchCalled = true
	s.gotCountFilter = filter
	return s.searchTotalCount, s.searchCountErr
}

func (s *stubEventRepository) ListMySummaries(_ context.Context, filter model.MyEventFilter, sort, order string, limit, offset int) ([]model.EventSummary, error) {
	s.listMyCalled = true
	s.gotMyFilter = filter
	s.gotMySort = sort
	s.gotMyOrder = order
	s.gotMyLimit = limit
	s.gotMyOffset = offset
	return s.myResults, s.myErr
}

func (s *stubEventRepository) CountMyEventKinds(_ context.Context, profileID uuid.UUID) (model.MyEventCounts, error) {
	s.countMyCalled = true
	s.gotMyCountProfileID = profileID
	return s.myCounts, s.myCountsErr
}

func (s *stubEventRepository) Create(_ context.Context, e *model.NewEvent) (model.CreateEventResponse, error) {
	s.gotNewEvent = e
	return s.createResult, s.createErr
}

func (s *stubEventRepository) GetOwnerProfileID(_ context.Context, _ string) (string, error) {
	return s.ownerProfileID, s.ownerProfileIDErr
}

func (s *stubEventRepository) GetTitle(_ context.Context, _ string) (string, error) {
	s.getTitleCalled = true
	return s.title, s.titleErr
}

func (s *stubEventRepository) GetByID(_ context.Context, _ string) (*model.EventResponse, error) {
	return nil, nil
}

func (s *stubEventRepository) Exists(_ context.Context, _ uuid.UUID) (bool, error) {
	return s.exists, s.existsErr
}

func (s *stubEventRepository) CancelWithNotification(_ context.Context, _ uuid.UUID, subject, body string) (time.Time, error) {
	s.gotCancelSubject = subject
	s.gotCancelBody = body
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

			got, err := svc.List(context.Background(), nil, nil, nil, nil, tt.inputSort, tt.inputOrder, tt.inputLimit, tt.inputOffset)

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

			got, err := svc.List(context.Background(), tt.inputKeywords, nil, nil, nil, tt.inputSort, tt.inputOrder, tt.inputLimit, tt.inputOffset)

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
				if !reflect.DeepEqual(stub.gotSearchFilter.Keywords, tt.wantKeywords) {
					t.Errorf("search keywords: got %#v, want %#v", stub.gotSearchFilter.Keywords, tt.wantKeywords)
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
				if !reflect.DeepEqual(stub.gotCountFilter.Keywords, tt.wantKeywords) {
					t.Errorf("count search keywords: got %#v, want %#v", stub.gotCountFilter.Keywords, tt.wantKeywords)
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
				if stub.gotSearchFilter.Keywords != nil {
					t.Errorf("SearchSummaries が呼ばれるべきではない: gotSearchFilter.Keywords=%#v", stub.gotSearchFilter.Keywords)
				}
			}

			if got.TotalCount != tt.wantTotal {
				t.Errorf("totalCount: got %d, want %d", got.TotalCount, tt.wantTotal)
			}
		})
	}
}

// TestEventQueryServiceList_TagFilter は tagIDs（OR 検索）の正規化・経路切り替え・
// 検証エラー（不正な UUID・件数超過）を検証する。
func TestEventQueryServiceList_TagFilter(t *testing.T) {
	t.Helper()

	searchResults := []model.EventSummary{
		{ID: "id-3", Title: "タグ絞り込みテストイベント", EventDate: time.Now().UTC(), CreatedAt: time.Now().UTC()},
	}
	const searchTotal = 5

	listResults := []model.EventSummary{
		{ID: "id-1", Title: "全件テストイベント", EventDate: time.Now().UTC(), CreatedAt: time.Now().UTC()},
	}
	const listTotal = 42

	const tagA = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	const tagB = "d290f1ee-6c54-4b01-90e6-d701748f0851"

	// 上限ちょうど・超過ケース用の UUID 群を用意する。
	exactlyMaxTagIDs := make([]string, 0, maxFilterTagIDs)
	for range maxFilterTagIDs {
		exactlyMaxTagIDs = append(exactlyMaxTagIDs, uuid.New().String())
	}
	overMaxTagIDs := make([]string, 0, maxFilterTagIDs+1)
	overMaxTagIDs = append(overMaxTagIDs, exactlyMaxTagIDs...)
	overMaxTagIDs = append(overMaxTagIDs, uuid.New().String())

	// 同一タグIDを重複指定するケース用（重複除去後は1件になり上限に達しない）。
	duplicateTagID := uuid.New().String()
	duplicateManyTagIDs := make([]string, 0, maxFilterTagIDs+1)
	for range maxFilterTagIDs + 1 {
		duplicateManyTagIDs = append(duplicateManyTagIDs, duplicateTagID)
	}

	tests := []struct {
		name             string
		inputKeywords    []string
		inputTagIDs      []string
		wantErr          bool
		wantErrMessage   string
		wantSearchCalled bool
		wantKeywords     []string
		wantTagIDs       []string
	}{
		{
			name:             "正常: タグIDのみ指定すると検索経路に入り正準形で渡る(Keywordsは空)",
			inputTagIDs:      []string{tagA},
			wantSearchCalled: true,
			wantKeywords:     nil,
			wantTagIDs:       []string{tagA},
		},
		{
			name:             "正常: qとタグIDを併用すると両方セットされ検索経路に入る",
			inputKeywords:    []string{"桜"},
			inputTagIDs:      []string{tagA},
			wantSearchCalled: true,
			wantKeywords:     []string{"桜"},
			wantTagIDs:       []string{tagA},
		},
		{
			name:             "正常: タグIDが空文字・空白のみは除去され他に条件が無ければ全件経路に入る",
			inputTagIDs:      []string{"", "   "},
			wantSearchCalled: false,
		},
		{
			name:             "正常: 大文字UUID・ブレース付き・重複指定は正準形へ正規化され重複除去される(順序保持)",
			inputTagIDs:      []string{"A1B2C3D4-E5F6-7890-ABCD-EF1234567890", tagB, "{a1b2c3d4-e5f6-7890-abcd-ef1234567890}"},
			wantSearchCalled: true,
			wantTagIDs:       []string{tagA, tagB},
		},
		{
			name:           "異常: UUID形式でない値は ValidationError になり repository は呼ばれない",
			inputTagIDs:    []string{"not-a-uuid"},
			wantErr:        true,
			wantErrMessage: "タグID[0]の形式が不正です",
		},
		{
			name:           "異常: 検証エラーメッセージのインデックスは元スライス基準(先頭が空文字・2番目が不正UUID)",
			inputTagIDs:    []string{"", "not-a-uuid"},
			wantErr:        true,
			wantErrMessage: "タグID[1]の形式が不正です",
		},
		{
			name:             "正常: タグIDがちょうど20件は成功する",
			inputTagIDs:      exactlyMaxTagIDs,
			wantSearchCalled: true,
			wantTagIDs:       exactlyMaxTagIDs,
		},
		{
			name:           "異常: タグIDが21件は ValidationError になる",
			inputTagIDs:    overMaxTagIDs,
			wantErr:        true,
			wantErrMessage: "タグIDは20件以内で指定してください",
		},
		{
			name:             "正常: 同じタグIDを21回重複指定しても重複除去後1件なのでエラーにならない",
			inputTagIDs:      duplicateManyTagIDs,
			wantSearchCalled: true,
			wantTagIDs:       []string{duplicateTagID},
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
			}
			svc := NewEventQueryService(stub, "")

			got, err := svc.List(context.Background(), tt.inputKeywords, tt.inputTagIDs, nil, nil, "", "", 0, 0)

			if tt.wantErr {
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("*ValidationError を期待したが got=%v", err)
				}
				if ve.Message != tt.wantErrMessage {
					t.Errorf("message: got %q, want %q", ve.Message, tt.wantErrMessage)
				}
				if stub.listCalled || stub.countCalled || stub.searchCalled || stub.countSearchCalled {
					t.Errorf("検証エラー時は repository が一度も呼ばれないべき: list=%v count=%v search=%v countSearch=%v",
						stub.listCalled, stub.countCalled, stub.searchCalled, stub.countSearchCalled)
				}
				return
			}
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}

			if tt.wantSearchCalled {
				if !stub.searchCalled || !stub.countSearchCalled {
					t.Errorf("検索経路が呼ばれるべき: searchCalled=%v countSearchCalled=%v", stub.searchCalled, stub.countSearchCalled)
				}
				if stub.listCalled || stub.countCalled {
					t.Errorf("全件経路は呼ばれるべきではない: listCalled=%v countCalled=%v", stub.listCalled, stub.countCalled)
				}
				if !reflect.DeepEqual(stub.gotSearchFilter.Keywords, tt.wantKeywords) {
					t.Errorf("filter.Keywords: got %#v, want %#v", stub.gotSearchFilter.Keywords, tt.wantKeywords)
				}
				if !reflect.DeepEqual(stub.gotSearchFilter.TagIDs, tt.wantTagIDs) {
					t.Errorf("filter.TagIDs: got %#v, want %#v", stub.gotSearchFilter.TagIDs, tt.wantTagIDs)
				}
				if !reflect.DeepEqual(stub.gotCountFilter.TagIDs, tt.wantTagIDs) {
					t.Errorf("count filter.TagIDs: got %#v, want %#v", stub.gotCountFilter.TagIDs, tt.wantTagIDs)
				}
				if got.TotalCount != searchTotal {
					t.Errorf("totalCount: got %d, want %d", got.TotalCount, searchTotal)
				}
			} else {
				if !stub.listCalled || !stub.countCalled {
					t.Errorf("全件経路が呼ばれるべき: listCalled=%v countCalled=%v", stub.listCalled, stub.countCalled)
				}
				if stub.searchCalled || stub.countSearchCalled {
					t.Errorf("検索経路は呼ばれるべきではない: searchCalled=%v countSearchCalled=%v", stub.searchCalled, stub.countSearchCalled)
				}
				if got.TotalCount != listTotal {
					t.Errorf("totalCount: got %d, want %d", got.TotalCount, listTotal)
				}
			}
		})
	}
}

// TestNormalizeStatuses は開催状況(status)の正規化ルール（ADR-0027）を検証する。
// trim・空要素除去、許可値との完全一致判定、重複除去、定義順（upcoming→ongoing→ended）への
// 並べ替え、許可値以外が *ValidationError になることを網羅する。
func TestNormalizeStatuses(t *testing.T) {
	t.Helper()

	tests := []struct {
		name           string
		input          []string
		want           []model.EventStatus
		wantErr        bool
		wantErrMessage string
	}{
		{
			name:  "正常: 空スライスは未指定としてnilを返す",
			input: nil,
			want:  nil,
		},
		{
			name:  "正常: 空要素・空白のみの要素は未指定として除去されnilを返す",
			input: []string{"", "   "},
			want:  nil,
		},
		{
			name:  "正常: 単一指定はそのまま1件になる",
			input: []string{"ongoing"},
			want:  []model.EventStatus{model.EventStatusOngoing},
		},
		{
			name:  "正常: 複数指定は入力順に依らず定義順(upcoming→ongoing→ended)へ並べ替える",
			input: []string{"ended", "upcoming"},
			want:  []model.EventStatus{model.EventStatusUpcoming, model.EventStatusEnded},
		},
		{
			name:  "正常: 3値すべて指定すると定義順3件になる",
			input: []string{"ended", "ongoing", "upcoming"},
			want: []model.EventStatus{
				model.EventStatusUpcoming, model.EventStatusOngoing, model.EventStatusEnded,
			},
		},
		{
			name:  "正常: 同一値の重複指定は除去され1件になる",
			input: []string{"upcoming", "upcoming", "upcoming"},
			want:  []model.EventStatus{model.EventStatusUpcoming},
		},
		{
			name:  "正常: 空要素と有効値が混在する場合は有効値だけ残る",
			input: []string{"", "ongoing", "  "},
			want:  []model.EventStatus{model.EventStatusOngoing},
		},
		{
			name:           "異常: 許可値以外は ValidationError になる",
			input:          []string{"invalid"},
			wantErr:        true,
			wantErrMessage: "開催状況(status)[0]の値が不正です",
		},
		{
			name:           "異常: 大文字表記(UPCOMING)は完全一致でないため不正値になる",
			input:          []string{"UPCOMING"},
			wantErr:        true,
			wantErrMessage: "開催状況(status)[0]の値が不正です",
		},
		{
			name:           "異常: エラーメッセージのインデックスは元スライス基準(先頭が空文字・2番目が不正値)",
			input:          []string{"", "invalid"},
			wantErr:        true,
			wantErrMessage: "開催状況(status)[1]の値が不正です",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeStatuses(tt.input)

			if tt.wantErr {
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("*ValidationError を期待したが got=%v", err)
				}
				if ve.Message != tt.wantErrMessage {
					t.Errorf("message: got %q, want %q", ve.Message, tt.wantErrMessage)
				}
				return
			}
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("normalizeStatuses(%#v) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

// TestEventQueryServiceList_StatusFilter は status（開催状況）の有無による経路切り替えと、
// 正規化後の値（重複除去・定義順への並べ替え）が repository へ渡ること、
// q・tagId との併用時も AND で検索経路に入ることを検証する（ADR-0027）。
func TestEventQueryServiceList_StatusFilter(t *testing.T) {
	t.Helper()

	searchResults := []model.EventSummary{
		{ID: "id-3", Title: "開催状況絞り込みテストイベント", EventDate: time.Now().UTC(), CreatedAt: time.Now().UTC()},
	}
	const searchTotal = 5

	listResults := []model.EventSummary{
		{ID: "id-1", Title: "全件テストイベント", EventDate: time.Now().UTC(), CreatedAt: time.Now().UTC()},
	}
	const listTotal = 42

	tests := []struct {
		name             string
		inputKeywords    []string
		inputTagIDs      []string
		inputStatuses    []string
		wantErr          bool
		wantErrMessage   string
		wantSearchCalled bool
		wantStatuses     []model.EventStatus
	}{
		{
			name:             "正常: statusのみ指定すると検索経路に入る(Keywords/TagIDsは空)",
			inputStatuses:    []string{"upcoming"},
			wantSearchCalled: true,
			wantStatuses:     []model.EventStatus{model.EventStatusUpcoming},
		},
		{
			name:             "正常: statusを複数指定すると重複除去・定義順で渡る",
			inputStatuses:    []string{"ended", "upcoming", "ended"},
			wantSearchCalled: true,
			wantStatuses:     []model.EventStatus{model.EventStatusUpcoming, model.EventStatusEnded},
		},
		{
			name:             "正常: q・tagId・statusを併用すると3条件とも検索経路に渡る(AND)",
			inputKeywords:    []string{"桜"},
			inputTagIDs:      []string{"a1b2c3d4-e5f6-7890-abcd-ef1234567890"},
			inputStatuses:    []string{"ongoing"},
			wantSearchCalled: true,
			wantStatuses:     []model.EventStatus{model.EventStatusOngoing},
		},
		{
			name:             "正常: statusが空文字・空白のみは未指定として扱われ他に条件が無ければ全件経路になる",
			inputStatuses:    []string{"", "  "},
			wantSearchCalled: false,
		},
		{
			name:           "異常: 許可値以外は ValidationError になり repository は呼ばれない",
			inputStatuses:  []string{"UPCOMING"},
			wantErr:        true,
			wantErrMessage: "開催状況(status)[0]の値が不正です",
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
			}
			svc := NewEventQueryService(stub, "")

			got, err := svc.List(context.Background(), tt.inputKeywords, tt.inputTagIDs, tt.inputStatuses, nil, "", "", 0, 0)

			if tt.wantErr {
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("*ValidationError を期待したが got=%v", err)
				}
				if ve.Message != tt.wantErrMessage {
					t.Errorf("message: got %q, want %q", ve.Message, tt.wantErrMessage)
				}
				if stub.listCalled || stub.countCalled || stub.searchCalled || stub.countSearchCalled {
					t.Errorf("検証エラー時は repository が一度も呼ばれないべき: list=%v count=%v search=%v countSearch=%v",
						stub.listCalled, stub.countCalled, stub.searchCalled, stub.countSearchCalled)
				}
				return
			}
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}

			if tt.wantSearchCalled {
				if !stub.searchCalled || !stub.countSearchCalled {
					t.Errorf("検索経路が呼ばれるべき: searchCalled=%v countSearchCalled=%v", stub.searchCalled, stub.countSearchCalled)
				}
				if stub.listCalled || stub.countCalled {
					t.Errorf("全件経路は呼ばれるべきではない: listCalled=%v countCalled=%v", stub.listCalled, stub.countCalled)
				}
				if !reflect.DeepEqual(stub.gotSearchFilter.Statuses, tt.wantStatuses) {
					t.Errorf("filter.Statuses: got %#v, want %#v", stub.gotSearchFilter.Statuses, tt.wantStatuses)
				}
				if !reflect.DeepEqual(stub.gotCountFilter.Statuses, tt.wantStatuses) {
					t.Errorf("count filter.Statuses: got %#v, want %#v", stub.gotCountFilter.Statuses, tt.wantStatuses)
				}
				if got.TotalCount != searchTotal {
					t.Errorf("totalCount: got %d, want %d", got.TotalCount, searchTotal)
				}
			} else {
				if !stub.listCalled || !stub.countCalled {
					t.Errorf("全件経路が呼ばれるべき: listCalled=%v countCalled=%v", stub.listCalled, stub.countCalled)
				}
				if stub.searchCalled || stub.countSearchCalled {
					t.Errorf("検索経路は呼ばれるべきではない: searchCalled=%v countSearchCalled=%v", stub.searchCalled, stub.countSearchCalled)
				}
				if got.TotalCount != listTotal {
					t.Errorf("totalCount: got %d, want %d", got.TotalCount, listTotal)
				}
			}
		})
	}
}

// TestNormalizeLocations は地域(location)絞り込みの正規化ルール（ADR-0028）を検証する。
// trim・空要素除去、NFKC正規化＋小文字化を判定キーとした重複除去、1要素あたりの文字数上限
// （maxLocationLength）超過・重複除去後の件数上限（maxFilterLocations）超過が
// *ValidationError になることを網羅する。
func TestNormalizeLocations(t *testing.T) {
	t.Helper()

	// 上限ちょうど・超過ケース用の一意な値群を用意する。
	exactlyMaxLocations := make([]string, 0, maxFilterLocations)
	for i := range maxFilterLocations {
		exactlyMaxLocations = append(exactlyMaxLocations, "loc"+strconv.Itoa(i))
	}
	overMaxLocations := make([]string, 0, maxFilterLocations+1)
	overMaxLocations = append(overMaxLocations, exactlyMaxLocations...)
	overMaxLocations = append(overMaxLocations, "loc"+strconv.Itoa(maxFilterLocations))

	// 同一語(全角/半角・大文字小文字違い)を201回以上重複指定するケース用
	// （重複除去後は1件になり上限に達しない。ADR-0028 決定4）。
	duplicateManyLocationVariants := []string{"Ｔｏｋｙｏ", "TOKYO", "tokyo"}
	duplicateManyLocations := make([]string, 0, maxFilterLocations+3)
	for i := range maxFilterLocations + 3 {
		duplicateManyLocations = append(duplicateManyLocations, duplicateManyLocationVariants[i%len(duplicateManyLocationVariants)])
	}

	// 一意な値200件に、それらの大文字小文字違いの重複を追加するケース用
	// （合計は201件を超えるが、重複除去後は200件のまま上限に達しない。ADR-0028 決定4）。
	uniqueLocationsWithDuplicates := make([]string, 0, len(exactlyMaxLocations)+3)
	uniqueLocationsWithDuplicates = append(uniqueLocationsWithDuplicates, exactlyMaxLocations...)
	uniqueLocationsWithDuplicates = append(uniqueLocationsWithDuplicates, "LOC0", "Loc1", "LOC2")

	exactlyMaxLength := strings.Repeat("あ", maxLocationLength)
	overMaxLength := strings.Repeat("あ", maxLocationLength+1)

	tests := []struct {
		name           string
		input          []string
		want           []string
		wantErr        bool
		wantErrMessage string
	}{
		{
			name:  "正常: 空スライスは未指定としてnilを返す",
			input: nil,
			want:  nil,
		},
		{
			name:  "正常: 空要素・空白のみの要素は未指定として除去されnilを返す",
			input: []string{"", "   "},
			want:  nil,
		},
		{
			name:  "正常: 単一指定はトリムされて1件になる",
			input: []string{"  東京都  "},
			want:  []string{"東京都"},
		},
		{
			name:  "正常: 複数指定は入力順のまま複数件になる",
			input: []string{"東京都", "神奈川県"},
			want:  []string{"東京都", "神奈川県"},
		},
		{
			name:  "正常: 全角/半角・大文字小文字が異なる同一語は重複除去され最初の入力値が残る",
			input: []string{"Ｔｏｋｙｏ", "TOKYO", "tokyo"},
			want:  []string{"Ｔｏｋｙｏ"},
		},
		{
			name:  "正常: 完全一致の重複指定は除去され1件になる",
			input: []string{"東京都", "東京都", "東京都"},
			want:  []string{"東京都"},
		},
		{
			name:  "正常: 地域がちょうど200件は成功する",
			input: exactlyMaxLocations,
			want:  exactlyMaxLocations,
		},
		{
			name:           "異常: 地域が201件はValidationErrorになる",
			input:          overMaxLocations,
			wantErr:        true,
			wantErrMessage: fmt.Sprintf("地域(location)は%d件以内で指定してください", maxFilterLocations),
		},
		{
			name:  "正常: 全角/半角・大文字小文字違いを含む同一語を201回以上重複指定しても重複除去後1件なのでエラーにならない",
			input: duplicateManyLocations,
			want:  []string{duplicateManyLocationVariants[0]},
		},
		{
			name:  "正常: 一意な値200件+それらの大文字小文字違いの重複で合計が201件を超えても重複除去後200件以内ならエラーにならない",
			input: uniqueLocationsWithDuplicates,
			want:  exactlyMaxLocations,
		},
		{
			name:  "正常: 1要素がちょうど255文字は成功する",
			input: []string{exactlyMaxLength},
			want:  []string{exactlyMaxLength},
		},
		{
			name:           "異常: 1要素が256文字はValidationErrorになる",
			input:          []string{overMaxLength},
			wantErr:        true,
			wantErrMessage: fmt.Sprintf("地域(location)[0]は%d文字以内で指定してください", maxLocationLength),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeLocations(tt.input)

			if tt.wantErr {
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("*ValidationError を期待したが got=%v", err)
				}
				if ve.Message != tt.wantErrMessage {
					t.Errorf("message: got %q, want %q", ve.Message, tt.wantErrMessage)
				}
				return
			}
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("normalizeLocations(%#v) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

// TestEventQueryServiceList_LocationFilter は location（地域）の有無による経路切り替えと、
// 正規化後の値（trim・重複除去）が repository へ渡ること、q・tagId・status との併用時も
// AND で検索経路に入ることを検証する（ADR-0028）。
func TestEventQueryServiceList_LocationFilter(t *testing.T) {
	t.Helper()

	// 上限超過ケース用の一意な値群を用意する。
	overMaxLocations := make([]string, 0, maxFilterLocations+1)
	for i := range maxFilterLocations + 1 {
		overMaxLocations = append(overMaxLocations, "loc"+strconv.Itoa(i))
	}

	searchResults := []model.EventSummary{
		{ID: "id-4", Title: "地域絞り込みテストイベント", EventDate: time.Now().UTC(), CreatedAt: time.Now().UTC()},
	}
	const searchTotal = 5

	listResults := []model.EventSummary{
		{ID: "id-1", Title: "全件テストイベント", EventDate: time.Now().UTC(), CreatedAt: time.Now().UTC()},
	}
	const listTotal = 42

	tests := []struct {
		name             string
		inputKeywords    []string
		inputTagIDs      []string
		inputStatuses    []string
		inputLocations   []string
		wantErr          bool
		wantErrMessage   string
		wantSearchCalled bool
		wantLocations    []string
	}{
		{
			name:             "正常: locationのみ指定すると検索経路に入る(Keywords/TagIDs/Statusesは空)",
			inputLocations:   []string{"東京都"},
			wantSearchCalled: true,
			wantLocations:    []string{"東京都"},
		},
		{
			name:             "正常: locationを複数指定するとORで検索経路に入る",
			inputLocations:   []string{"東京都", "神奈川県"},
			wantSearchCalled: true,
			wantLocations:    []string{"東京都", "神奈川県"},
		},
		{
			name:             "正常: q・tagId・status・locationを併用すると4条件とも検索経路に渡る(AND)",
			inputKeywords:    []string{"桜"},
			inputTagIDs:      []string{"a1b2c3d4-e5f6-7890-abcd-ef1234567890"},
			inputStatuses:    []string{"ongoing"},
			inputLocations:   []string{"東京都"},
			wantSearchCalled: true,
			wantLocations:    []string{"東京都"},
		},
		{
			name:             "正常: locationが空文字・空白のみは未指定として扱われ他に条件が無ければ全件経路になる",
			inputLocations:   []string{"", "  "},
			wantSearchCalled: false,
		},
		{
			name:           "異常: 201件以上の指定はValidationErrorになり repository は呼ばれない",
			inputLocations: overMaxLocations,
			wantErr:        true,
			wantErrMessage: fmt.Sprintf("地域(location)は%d件以内で指定してください", maxFilterLocations),
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
			}
			svc := NewEventQueryService(stub, "")

			got, err := svc.List(context.Background(), tt.inputKeywords, tt.inputTagIDs, tt.inputStatuses, tt.inputLocations, "", "", 0, 0)

			if tt.wantErr {
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("*ValidationError を期待したが got=%v", err)
				}
				if ve.Message != tt.wantErrMessage {
					t.Errorf("message: got %q, want %q", ve.Message, tt.wantErrMessage)
				}
				if stub.listCalled || stub.countCalled || stub.searchCalled || stub.countSearchCalled {
					t.Errorf("検証エラー時は repository が一度も呼ばれないべき: list=%v count=%v search=%v countSearch=%v",
						stub.listCalled, stub.countCalled, stub.searchCalled, stub.countSearchCalled)
				}
				return
			}
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}

			if tt.wantSearchCalled {
				if !stub.searchCalled || !stub.countSearchCalled {
					t.Errorf("検索経路が呼ばれるべき: searchCalled=%v countSearchCalled=%v", stub.searchCalled, stub.countSearchCalled)
				}
				if stub.listCalled || stub.countCalled {
					t.Errorf("全件経路は呼ばれるべきではない: listCalled=%v countCalled=%v", stub.listCalled, stub.countCalled)
				}
				if !reflect.DeepEqual(stub.gotSearchFilter.Locations, tt.wantLocations) {
					t.Errorf("filter.Locations: got %#v, want %#v", stub.gotSearchFilter.Locations, tt.wantLocations)
				}
				if !reflect.DeepEqual(stub.gotCountFilter.Locations, tt.wantLocations) {
					t.Errorf("count filter.Locations: got %#v, want %#v", stub.gotCountFilter.Locations, tt.wantLocations)
				}
				if got.TotalCount != searchTotal {
					t.Errorf("totalCount: got %d, want %d", got.TotalCount, searchTotal)
				}
			} else {
				if !stub.listCalled || !stub.countCalled {
					t.Errorf("全件経路が呼ばれるべき: listCalled=%v countCalled=%v", stub.listCalled, stub.countCalled)
				}
				if stub.searchCalled || stub.countSearchCalled {
					t.Errorf("検索経路は呼ばれるべきではない: searchCalled=%v countSearchCalled=%v", stub.searchCalled, stub.countSearchCalled)
				}
				if got.TotalCount != listTotal {
					t.Errorf("totalCount: got %d, want %d", got.TotalCount, listTotal)
				}
			}
		})
	}
}

// testMyEventProfileID は ListByProfile 系テストで使い回す固定のプロフィールUUID(ADR-0010)。
var testMyEventProfileID = uuid.MustParse("d290f1ee-6c54-4b01-90e6-d701748f0851")

// TestEventQueryServiceListByProfile_InvalidKind は type が不正な場合に *ValidationError を
// 返し、repository のいずれのメソッドも呼ばれないことを検証する。
func TestEventQueryServiceListByProfile_InvalidKind(t *testing.T) {
	t.Helper()

	tests := []struct {
		name string
		kind string
	}{
		{name: "空文字は不正", kind: ""},
		{name: "大文字は不正(小文字定義のみ許可)", kind: "HOSTED"},
		{name: "未定義の種別は不正", kind: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubEventRepository{}
			svc := NewEventQueryService(stub, "")

			_, err := svc.ListByProfile(context.Background(), testMyEventProfileID, tt.kind, "", "", 0, 0)

			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("*ValidationError を期待したが got=%v", err)
			}
			if stub.listMyCalled || stub.countMyCalled {
				t.Errorf("検証エラー時は repository が呼ばれないべき: listMyCalled=%v countMyCalled=%v", stub.listMyCalled, stub.countMyCalled)
			}
		})
	}
}

// TestEventQueryServiceListByProfile_FilterByKind は種別ごとに repository へ渡す
// model.MyEventFilter（ProfileID・Kind）が期待どおりであること、CountMyEventKinds に
// 渡る profileID が一致することを検証する。
func TestEventQueryServiceListByProfile_FilterByKind(t *testing.T) {
	t.Helper()

	profileID := testMyEventProfileID

	tests := []struct {
		name     string
		kind     string
		wantKind model.MyEventKind
	}{
		{name: "hosted", kind: "hosted", wantKind: model.MyEventKindHosted},
		{name: "applied", kind: "applied", wantKind: model.MyEventKindApplied},
		{name: "attended", kind: "attended", wantKind: model.MyEventKindAttended},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubEventRepository{}
			svc := NewEventQueryService(stub, "")

			_, err := svc.ListByProfile(context.Background(), profileID, tt.kind, "", "", 0, 0)
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}

			wantFilter := model.MyEventFilter{ProfileID: profileID, Kind: tt.wantKind}
			if stub.gotMyFilter != wantFilter {
				t.Errorf("filter: got %#v, want %#v", stub.gotMyFilter, wantFilter)
			}
			if stub.gotMyCountProfileID != profileID {
				t.Errorf("CountMyEventKinds の profileID: got %q, want %q", stub.gotMyCountProfileID, profileID)
			}
		})
	}
}

// TestEventQueryServiceListByProfile_Normalization は limit/offset/sort/order の
// 正規化結果が repository の ListMySummaries に渡ることを検証する。
func TestEventQueryServiceListByProfile_Normalization(t *testing.T) {
	t.Helper()

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
	}{
		{
			name:       "limit/offset がデフォルト値(0)の場合は default20/0 に正規化",
			wantSort:   "created_at",
			wantOrder:  "desc",
			wantLimit:  20,
			wantOffset: 0,
		},
		{
			name:       "limit が 101 の場合は 100 に丸める",
			inputLimit: 101,
			wantSort:   "created_at",
			wantOrder:  "desc",
			wantLimit:  100,
		},
		{
			name:        "offset が負値の場合は 0 に丸める",
			inputOffset: -1,
			wantSort:    "created_at",
			wantOrder:   "desc",
			wantLimit:   20,
			wantOffset:  0,
		},
		{
			name:      "sort が不正値の場合は created_at にデフォルト",
			inputSort: "invalid_column",
			wantSort:  "created_at",
			wantOrder: "desc",
			wantLimit: 20,
		},
		{
			name:       "order が不正値の場合は desc にデフォルト",
			inputOrder: "invalid_order",
			wantSort:   "created_at",
			wantOrder:  "desc",
			wantLimit:  20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubEventRepository{}
			svc := NewEventQueryService(stub, "")

			got, err := svc.ListByProfile(context.Background(), testMyEventProfileID, "hosted", tt.inputSort, tt.inputOrder, tt.inputLimit, tt.inputOffset)
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}

			if stub.gotMySort != tt.wantSort {
				t.Errorf("sort: got %q, want %q", stub.gotMySort, tt.wantSort)
			}
			if stub.gotMyOrder != tt.wantOrder {
				t.Errorf("order: got %q, want %q", stub.gotMyOrder, tt.wantOrder)
			}
			if stub.gotMyLimit != tt.wantLimit {
				t.Errorf("limit: got %d, want %d", stub.gotMyLimit, tt.wantLimit)
			}
			if stub.gotMyOffset != tt.wantOffset {
				t.Errorf("offset: got %d, want %d", stub.gotMyOffset, tt.wantOffset)
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

// TestEventQueryServiceListByProfile_Counts は repository が返す counts がそのまま
// レスポンスに入ること、TotalCount がリクエストした種別の counts.Of(kind) と
// 一致することを検証する。
func TestEventQueryServiceListByProfile_Counts(t *testing.T) {
	t.Helper()

	counts := model.MyEventCounts{Hosted: 4, Applied: 2, Attended: 3}

	tests := []struct {
		name      string
		kind      string
		wantTotal int
	}{
		{name: "hosted", kind: "hosted", wantTotal: 4},
		{name: "applied", kind: "applied", wantTotal: 2},
		{name: "attended", kind: "attended", wantTotal: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubEventRepository{myCounts: counts}
			svc := NewEventQueryService(stub, "")

			got, err := svc.ListByProfile(context.Background(), testMyEventProfileID, tt.kind, "", "", 0, 0)
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}

			if got.Counts != counts {
				t.Errorf("Counts: got %#v, want %#v", got.Counts, counts)
			}
			if got.TotalCount != tt.wantTotal {
				t.Errorf("TotalCount: got %d, want %d", got.TotalCount, tt.wantTotal)
			}
		})
	}
}

// TestEventQueryServiceListByProfile_RepositoryErrors は ListMySummaries /
// CountMyEventKinds のエラーがそのまま呼び出し元に伝播することを検証する。
// ListMySummaries がエラーの場合は CountMyEventKinds が呼ばれないことも確認する。
func TestEventQueryServiceListByProfile_RepositoryErrors(t *testing.T) {
	t.Helper()

	t.Run("ListMySummaries のエラーが伝播し CountMyEventKinds は呼ばれない", func(t *testing.T) {
		stub := &stubEventRepository{myErr: errors.New("list my db error")}
		svc := NewEventQueryService(stub, "")

		_, err := svc.ListByProfile(context.Background(), testMyEventProfileID, "hosted", "", "", 0, 0)
		if err == nil {
			t.Fatal("エラーを期待したが nil だった")
		}
		if stub.countMyCalled {
			t.Error("ListMySummaries がエラーの場合 CountMyEventKinds は呼ばれないべき")
		}
	})

	t.Run("CountMyEventKinds のエラーが伝播する", func(t *testing.T) {
		stub := &stubEventRepository{myCountsErr: errors.New("count my db error")}
		svc := NewEventQueryService(stub, "")

		_, err := svc.ListByProfile(context.Background(), testMyEventProfileID, "hosted", "", "", 0, 0)
		if err == nil {
			t.Fatal("エラーを期待したが nil だった")
		}
		if !stub.listMyCalled {
			t.Error("CountMyEventKinds のエラー検証前に ListMySummaries が呼ばれているべき")
		}
	})
}

// TestEventQueryServiceListPublicByProfile_InvalidKind は type が公開対象外
// （applied・空文字・未知の値）の場合に *ValidationError を返し、repository の
// いずれのメソッドも呼ばれないことを検証する。
func TestEventQueryServiceListPublicByProfile_InvalidKind(t *testing.T) {
	t.Helper()

	tests := []struct {
		name string
		kind string
	}{
		{name: "applied は本人限定のため不正", kind: "applied"},
		{name: "空文字は不正", kind: ""},
		{name: "大文字は不正(小文字定義のみ許可)", kind: "HOSTED"},
		{name: "未定義の種別は不正", kind: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubEventRepository{}
			svc := NewEventQueryService(stub, "")

			_, err := svc.ListPublicByProfile(context.Background(), testMyEventProfileID, tt.kind, "", "", 0, 0)

			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("*ValidationError を期待したが got=%v", err)
			}
			if stub.listMyCalled || stub.countMyCalled {
				t.Errorf("検証エラー時は repository が呼ばれないべき: listMyCalled=%v countMyCalled=%v", stub.listMyCalled, stub.countMyCalled)
			}
		})
	}
}

// TestEventQueryServiceListPublicByProfile_FilterByKind は公開対象の種別(hosted/attended)
// ごとに repository へ渡す model.MyEventFilter（ProfileID・Kind）が期待どおりであること、
// CountMyEventKinds に渡る profileID が一致することを検証する。
func TestEventQueryServiceListPublicByProfile_FilterByKind(t *testing.T) {
	t.Helper()

	profileID := testMyEventProfileID

	tests := []struct {
		name     string
		kind     string
		wantKind model.MyEventKind
	}{
		{name: "hosted", kind: "hosted", wantKind: model.MyEventKindHosted},
		{name: "attended", kind: "attended", wantKind: model.MyEventKindAttended},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubEventRepository{}
			svc := NewEventQueryService(stub, "")

			_, err := svc.ListPublicByProfile(context.Background(), profileID, tt.kind, "", "", 0, 0)
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}

			wantFilter := model.MyEventFilter{ProfileID: profileID, Kind: tt.wantKind}
			if stub.gotMyFilter != wantFilter {
				t.Errorf("filter: got %#v, want %#v", stub.gotMyFilter, wantFilter)
			}
			if stub.gotMyCountProfileID != profileID {
				t.Errorf("CountMyEventKinds の profileID: got %q, want %q", stub.gotMyCountProfileID, profileID)
			}
		})
	}
}

// TestEventQueryServiceListPublicByProfile_Normalization は limit/offset/sort/order の
// 正規化結果が repository の ListMySummaries に渡ることを検証する。
func TestEventQueryServiceListPublicByProfile_Normalization(t *testing.T) {
	t.Helper()

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
	}{
		{
			name:       "limit/offset がデフォルト値(0)の場合は default20/0 に正規化",
			wantSort:   "created_at",
			wantOrder:  "desc",
			wantLimit:  20,
			wantOffset: 0,
		},
		{
			name:       "limit が 101 の場合は 100 に丸める",
			inputLimit: 101,
			wantSort:   "created_at",
			wantOrder:  "desc",
			wantLimit:  100,
		},
		{
			name:        "offset が負値の場合は 0 に丸める",
			inputOffset: -1,
			wantSort:    "created_at",
			wantOrder:   "desc",
			wantLimit:   20,
			wantOffset:  0,
		},
		{
			name:      "sort が不正値の場合は created_at にデフォルト",
			inputSort: "invalid_column",
			wantSort:  "created_at",
			wantOrder: "desc",
			wantLimit: 20,
		},
		{
			name:       "order が不正値の場合は desc にデフォルト",
			inputOrder: "invalid_order",
			wantSort:   "created_at",
			wantOrder:  "desc",
			wantLimit:  20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubEventRepository{}
			svc := NewEventQueryService(stub, "")

			got, err := svc.ListPublicByProfile(context.Background(), testMyEventProfileID, "hosted", tt.inputSort, tt.inputOrder, tt.inputLimit, tt.inputOffset)
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}

			if stub.gotMySort != tt.wantSort {
				t.Errorf("sort: got %q, want %q", stub.gotMySort, tt.wantSort)
			}
			if stub.gotMyOrder != tt.wantOrder {
				t.Errorf("order: got %q, want %q", stub.gotMyOrder, tt.wantOrder)
			}
			if stub.gotMyLimit != tt.wantLimit {
				t.Errorf("limit: got %d, want %d", stub.gotMyLimit, tt.wantLimit)
			}
			if stub.gotMyOffset != tt.wantOffset {
				t.Errorf("offset: got %d, want %d", stub.gotMyOffset, tt.wantOffset)
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

// TestEventQueryServiceListPublicByProfile_Counts は repository が返す
// MyEventCounts から applied を除いた ProfileEventCounts がレスポンスに入ること、
// TotalCount がリクエストした種別の counts.Of(kind) と一致することを検証する。
func TestEventQueryServiceListPublicByProfile_Counts(t *testing.T) {
	t.Helper()

	repoCounts := model.MyEventCounts{Hosted: 4, Applied: 2, Attended: 3}
	wantCounts := model.ProfileEventCounts{Hosted: 4, Attended: 3}

	tests := []struct {
		name      string
		kind      string
		wantTotal int
	}{
		{name: "hosted", kind: "hosted", wantTotal: 4},
		{name: "attended", kind: "attended", wantTotal: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubEventRepository{myCounts: repoCounts}
			svc := NewEventQueryService(stub, "")

			got, err := svc.ListPublicByProfile(context.Background(), testMyEventProfileID, tt.kind, "", "", 0, 0)
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}

			if got.Counts != wantCounts {
				t.Errorf("Counts: got %#v, want %#v(applied を含まないこと)", got.Counts, wantCounts)
			}
			if got.TotalCount != tt.wantTotal {
				t.Errorf("TotalCount: got %d, want %d", got.TotalCount, tt.wantTotal)
			}
		})
	}
}

// TestEventQueryServiceListPublicByProfile_RepositoryErrors は ListMySummaries /
// CountMyEventKinds のエラーがそのまま呼び出し元に伝播することを検証する。
// ListMySummaries がエラーの場合は CountMyEventKinds が呼ばれないことも確認する。
func TestEventQueryServiceListPublicByProfile_RepositoryErrors(t *testing.T) {
	t.Helper()

	t.Run("ListMySummaries のエラーが伝播し CountMyEventKinds は呼ばれない", func(t *testing.T) {
		stub := &stubEventRepository{myErr: errors.New("list my db error")}
		svc := NewEventQueryService(stub, "")

		_, err := svc.ListPublicByProfile(context.Background(), testMyEventProfileID, "hosted", "", "", 0, 0)
		if err == nil {
			t.Fatal("エラーを期待したが nil だった")
		}
		if stub.countMyCalled {
			t.Error("ListMySummaries がエラーの場合 CountMyEventKinds は呼ばれないべき")
		}
	})

	t.Run("CountMyEventKinds のエラーが伝播する", func(t *testing.T) {
		stub := &stubEventRepository{myCountsErr: errors.New("count my db error")}
		svc := NewEventQueryService(stub, "")

		_, err := svc.ListPublicByProfile(context.Background(), testMyEventProfileID, "hosted", "", "", 0, 0)
		if err == nil {
			t.Fatal("エラーを期待したが nil だった")
		}
		if !stub.listMyCalled {
			t.Error("CountMyEventKinds のエラー検証前に ListMySummaries が呼ばれているべき")
		}
	})
}
