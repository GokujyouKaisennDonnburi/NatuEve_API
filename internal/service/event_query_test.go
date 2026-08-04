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
	// 各メソッドが実際に呼ばれたかどうかの記録（経路切り替え・未呼び出しの検証に使う）。
	listCalled        bool
	countCalled       bool
	searchCalled      bool
	countSearchCalled bool
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

			got, err := svc.List(context.Background(), nil, nil, tt.inputSort, tt.inputOrder, tt.inputLimit, tt.inputOffset)

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

			got, err := svc.List(context.Background(), tt.inputKeywords, nil, tt.inputSort, tt.inputOrder, tt.inputLimit, tt.inputOffset)

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

			got, err := svc.List(context.Background(), tt.inputKeywords, tt.inputTagIDs, "", "", 0, 0)

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
