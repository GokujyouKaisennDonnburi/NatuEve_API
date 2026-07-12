package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/model"
	"github.com/GokujyouKaisennDonnburi/NatuEve_API/internal/repository"
)

// assertNoErr はテストヘルパー: エラーが nil でなければ fatal する。
func assertNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
}

// assertValidationError はテストヘルパー: err が *ValidationError であることを確認する。
func assertValidationError(t *testing.T, err error) *ValidationError {
	t.Helper()
	if err == nil {
		t.Fatal("ValidationError を期待したが nil だった")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("*ValidationError を期待したが %T だった: %v", err, err)
	}
	return ve
}

// validRequest は正常系テスト用の最小限の有効なリクエスト。
func validRequest() model.CreateEventRequest {
	return model.CreateEventRequest{
		Title:       "サクラ観察会",
		Description: "春の桜を観察するイベントです。",
		Location:    "東京都新宿御苑",
		EventDate:   time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
		Costs: []model.EventCostInput{
			{Category: "参加費", Cost: 500},
		},
	}
}

func TestEventCommandServiceCreate_Validation(t *testing.T) {
	dummyResp := model.CreateEventResponse{
		ID:        "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		CreatedAt: time.Now().UTC(),
	}
	const profileID = "profile-uuid-001"

	tests := []struct {
		name       string
		req        model.CreateEventRequest
		stubErr    error
		wantValErr bool
		wantErr    bool
		// 正常系: stub に渡った NewEvent の検証用。
		checkNewEvent func(t *testing.T, e *model.NewEvent)
	}{
		{
			name: "正常: 全必須フィールドあり",
			req:  validRequest(),
			checkNewEvent: func(t *testing.T, e *model.NewEvent) {
				t.Helper()
				if e == nil {
					t.Fatal("NewEvent が nil")
				}
				if e.ProfileID != profileID {
					t.Errorf("ProfileID: got %q, want %q", e.ProfileID, profileID)
				}
				if e.Title != "サクラ観察会" {
					t.Errorf("Title: got %q, want %q", e.Title, "サクラ観察会")
				}
				if len(e.Costs) != 1 || e.Costs[0].Category != "参加費" || e.Costs[0].Cost != 500 {
					t.Errorf("Costs: got %v", e.Costs)
				}
			},
		},
		{
			// 画像・PDF キーは昇格フロー（promoteObjects）で変換されるため、
			// この正常系テスト（store=nil）ではキーなしで Capacity/ExternalURL/Items の変換のみ確認する。
			name: "正常: 任意フィールドあり（Capacity・ExternalURL・Items）",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.Capacity = 30
				r.ExternalURL = "https://example.com/event"
				r.Items = []model.EventItemInput{
					{Item: "双眼鏡", IsRequired: true},
				}
				return r
			}(),
			checkNewEvent: func(t *testing.T, e *model.NewEvent) {
				t.Helper()
				if e.Capacity != 30 {
					t.Errorf("Capacity: got %v, want 30", e.Capacity)
				}
				if e.ExternalURL != "https://example.com/event" {
					t.Errorf("ExternalURL: got %q", e.ExternalURL)
				}
				if len(e.Items) != 1 || e.Items[0].Item != "双眼鏡" {
					t.Errorf("Items: got %v", e.Items)
				}
			},
		},
		{
			name: "正常: title のトリムが反映される",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.Title = "  サクラ観察会  "
				return r
			}(),
			checkNewEvent: func(t *testing.T, e *model.NewEvent) {
				t.Helper()
				if e.Title != "サクラ観察会" {
					t.Errorf("Title trim: got %q", e.Title)
				}
			},
		},
		{
			name: "異常: title が空",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.Title = ""
				return r
			}(),
			wantValErr: true,
		},
		{
			name: "異常: title がスペースのみ",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.Title = "   "
				return r
			}(),
			wantValErr: true,
		},
		{
			name: "異常: title が 255 文字超過",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.Title = string(make([]rune, 256))
				for i := range r.Title {
					_ = i
				}
				// 256 文字の文字列を作る。
				runes := make([]rune, 256)
				for i := range runes {
					runes[i] = 'あ'
				}
				r.Title = string(runes)
				return r
			}(),
			wantValErr: true,
		},
		{
			name: "異常: description が空",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.Description = ""
				return r
			}(),
			wantValErr: true,
		},
		{
			name: "正常: description がちょうど 10,000 文字（rune）",
			req: func() model.CreateEventRequest {
				r := validRequest()
				runes := make([]rune, 10000)
				for i := range runes {
					runes[i] = 'あ'
				}
				r.Description = string(runes)
				return r
			}(),
		},
		{
			name: "異常: description が 10,001 文字（rune）",
			req: func() model.CreateEventRequest {
				r := validRequest()
				runes := make([]rune, 10001)
				for i := range runes {
					runes[i] = 'あ'
				}
				r.Description = string(runes)
				return r
			}(),
			wantValErr: true,
		},
		{
			name: "異常: location が空",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.Location = ""
				return r
			}(),
			wantValErr: true,
		},
		{
			name: "異常: location が 255 文字超過",
			req: func() model.CreateEventRequest {
				r := validRequest()
				runes := make([]rune, 256)
				for i := range runes {
					runes[i] = 'あ'
				}
				r.Location = string(runes)
				return r
			}(),
			wantValErr: true,
		},
		{
			name: "異常: eventDate がゼロ値",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.EventDate = time.Time{}
				return r
			}(),
			wantValErr: true,
		},
		{
			name: "異常: costs が空配列",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.Costs = []model.EventCostInput{}
				return r
			}(),
			wantValErr: true,
		},
		{
			name: "異常: costs が nil",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.Costs = nil
				return r
			}(),
			wantValErr: true,
		},
		{
			name: "異常: cost の category が空",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.Costs = []model.EventCostInput{{Category: "", Cost: 0}}
				return r
			}(),
			wantValErr: true,
		},
		{
			name: "異常: cost が負値",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.Costs = []model.EventCostInput{{Category: "参加費", Cost: -1}}
				return r
			}(),
			wantValErr: true,
		},
		{
			name: "正常: cost が 0 は許可",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.Costs = []model.EventCostInput{{Category: "参加費", Cost: 0}}
				return r
			}(),
		},
		{
			name: "正常: capacity が 0（未設定扱い）",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.Capacity = 0
				return r
			}(),
			checkNewEvent: func(t *testing.T, e *model.NewEvent) {
				t.Helper()
				if e.Capacity != 0 {
					t.Errorf("Capacity: got %v, want 0", e.Capacity)
				}
			},
		},
		{
			name: "正常: capacity が正数",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.Capacity = 50
				return r
			}(),
			checkNewEvent: func(t *testing.T, e *model.NewEvent) {
				t.Helper()
				if e.Capacity != 50 {
					t.Errorf("Capacity: got %v, want 50", e.Capacity)
				}
			},
		},
		{
			name: "異常: capacity が負値",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.Capacity = -1
				return r
			}(),
			wantValErr: true,
		},
		{
			name: "異常: externalUrl が不正スキーム(ftp)",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.ExternalURL = "ftp://example.com/event"
				return r
			}(),
			wantValErr: true,
		},
		{
			name: "異常: externalUrl がスキームなし",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.ExternalURL = "example.com/event"
				return r
			}(),
			wantValErr: true,
		},
		{
			name: "異常: imageObjectKey が空文字",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.ImageObjectKeys = []string{"valid-key", ""}
				return r
			}(),
			wantValErr: true,
		},
		{
			name: "異常: imageObjectKey がスペースのみ",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.ImageObjectKeys = []string{"  "}
				return r
			}(),
			wantValErr: true,
		},
		{
			name: "異常: item の名称が空",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.Items = []model.EventItemInput{{Item: "", IsRequired: false}}
				return r
			}(),
			wantValErr: true,
		},
		{
			name: "異常: tagId が不正なUUID形式",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.TagIDs = []string{"not-a-uuid"}
				return r
			}(),
			wantValErr: true,
		},
		{
			name: "異常: tagId が空文字",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.TagIDs = []string{"  "}
				return r
			}(),
			wantValErr: true,
		},
		{
			name: "正常: tagIds は trim・重複除去されて伝播する",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.TagIDs = []string{
					" a1b2c3d4-e5f6-7890-abcd-ef1234567890 ",
					"b2c3d4e5-f6a8-8901-bcde-f23456789013",
					"a1b2c3d4-e5f6-7890-abcd-ef1234567890",
				}
				return r
			}(),
			checkNewEvent: func(t *testing.T, e *model.NewEvent) {
				t.Helper()
				want := []string{
					"a1b2c3d4-e5f6-7890-abcd-ef1234567890",
					"b2c3d4e5-f6a8-8901-bcde-f23456789013",
				}
				if !reflect.DeepEqual(e.TagIDs, want) {
					t.Errorf("TagIDs: got %v, want %v", e.TagIDs, want)
				}
			},
		},
		{
			// urn:uuid: 形式は uuid.Parse を通過するが Postgres の uuid 型は拒否する。
			// 正準形へ正規化して伝播することを検証する（DB 書き込みでの 500 回避）。
			name: "正常: urn:uuid や大文字表記が正準形に正規化・重複除去される",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.TagIDs = []string{
					"urn:uuid:a1b2c3d4-e5f6-7890-abcd-ef1234567890",
					"A1B2C3D4-E5F6-7890-ABCD-EF1234567890",
				}
				return r
			}(),
			checkNewEvent: func(t *testing.T, e *model.NewEvent) {
				t.Helper()
				want := []string{"a1b2c3d4-e5f6-7890-abcd-ef1234567890"}
				if !reflect.DeepEqual(e.TagIDs, want) {
					t.Errorf("TagIDs: got %v, want %v", e.TagIDs, want)
				}
			},
		},
		{
			name:    "異常: repository の Create がエラーを返す",
			req:     validRequest(),
			stubErr: errors.New("db error"),
			wantErr: true,
		},
		{
			name: "異常: repository が ErrTagNotFound を返す",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.TagIDs = []string{"a1b2c3d4-e5f6-7890-abcd-ef1234567890"}
				return r
			}(),
			stubErr:    fmt.Errorf("insert event tag %s: %w", "a1b2c3d4-e5f6-7890-abcd-ef1234567890", repository.ErrTagNotFound),
			wantValErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubEventRepository{
				createResult: dummyResp,
				createErr:    tt.stubErr,
			}
			svc := NewEventCommandService(stub, nil, nil)

			got, err := svc.Create(context.Background(), profileID, tt.req)

			if tt.wantValErr {
				_ = assertValidationError(t, err)
				return
			}

			if tt.wantErr {
				if err == nil {
					t.Fatal("エラーを期待したが nil だった")
				}
				return
			}

			assertNoErr(t, err)

			if got.ID != dummyResp.ID {
				t.Errorf("ID: got %q, want %q", got.ID, dummyResp.ID)
			}

			// stub に渡った NewEvent を追加検証する。
			if tt.checkNewEvent != nil {
				tt.checkNewEvent(t, stub.gotNewEvent)
			}
		})
	}
}

// --- 昇格フロー（promoteObjects）テスト ---

const testProfileID = "profile-uuid-001"

// loadTestdata はテストデータファイルを読み込む。
func loadTestdata(t *testing.T, filename string) []byte {
	t.Helper()
	// filepath.Join を使い testdata/ 配下のみ参照することを保証する。
	p := filepath.Join("testdata", filepath.Base(filename))
	data, err := os.ReadFile(p) //nolint:gosec
	if err != nil {
		t.Fatalf("testdata/%s の読み込みに失敗: %v", filename, err)
	}
	return data
}

func TestEventCommandServiceCreate_OwnershipCheck(t *testing.T) {
	dummyResp := model.CreateEventResponse{ID: "event-001", CreatedAt: time.Now().UTC()}
	jpegData := loadTestdata(t, "sample.jpg")

	tests := []struct {
		name           string
		imageObjectKey string
		wantValErr     bool
	}{
		{
			name:           "正常: 自分の tmp prefix に属するキー",
			imageObjectKey: "natueve/tmp/" + testProfileID + "/uuid.jpg",
		},
		{
			name:           "異常: 他人の tmp prefix",
			imageObjectKey: "natueve/tmp/other-user-id/uuid.jpg",
			wantValErr:     true,
		},
		{
			name:           "異常: events/ に直接参照",
			imageObjectKey: "natueve/events/images/uuid.jpg",
			wantValErr:     true,
		},
		{
			name:           "異常: prefix なし（キーのみ）",
			imageObjectKey: "uuid.jpg",
			wantValErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeObjectStore{
				headSize:        int64(len(jpegData)),
				headContentType: "image/jpeg",
				getData:         jpegData,
			}
			repoStub := &stubEventRepository{
				createResult: dummyResp,
			}
			svc := NewEventCommandService(repoStub, store, nil)

			req := validRequest()
			req.ImageObjectKeys = []string{tt.imageObjectKey}

			_, err := svc.Create(context.Background(), testProfileID, req)
			if tt.wantValErr {
				_ = assertValidationError(t, err)
				return
			}
			assertNoErr(t, err)
		})
	}
}

func TestEventCommandServiceCreate_SizeLimit(t *testing.T) {
	dummyResp := model.CreateEventResponse{ID: "event-001", CreatedAt: time.Now().UTC()}
	validKey := "natueve/tmp/" + testProfileID + "/uuid.jpg"
	jpegData := loadTestdata(t, "sample.jpg")

	tests := []struct {
		name        string
		headSize    int64
		contentType string
		wantValErr  bool
	}{
		{
			name:        "正常: 画像 10MB 以内",
			headSize:    10 * 1024 * 1024,
			contentType: "image/jpeg",
		},
		{
			name:        "異常: 画像 10MB 超過",
			headSize:    10*1024*1024 + 1,
			contentType: "image/jpeg",
			wantValErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeObjectStore{
				headSize:        tt.headSize,
				headContentType: tt.contentType,
				getData:         jpegData,
			}
			repoStub := &stubEventRepository{createResult: dummyResp}
			svc := NewEventCommandService(repoStub, store, nil)

			req := validRequest()
			req.ImageObjectKeys = []string{validKey}

			_, err := svc.Create(context.Background(), testProfileID, req)
			if tt.wantValErr {
				_ = assertValidationError(t, err)
				return
			}
			assertNoErr(t, err)
		})
	}
}

func TestEventCommandServiceCreate_MagicNumberMismatch(t *testing.T) {
	dummyResp := model.CreateEventResponse{ID: "event-001", CreatedAt: time.Now().UTC()}
	validKey := "natueve/tmp/" + testProfileID + "/uuid.jpg"

	// PNG データを JPEG として宣言（マジックナンバー不一致）
	pngData := loadTestdata(t, "sample.png")

	store := &fakeObjectStore{
		headSize:        int64(len(pngData)),
		headContentType: "image/jpeg", // 宣言は JPEG だが実体は PNG
		getData:         pngData,
	}
	repoStub := &stubEventRepository{createResult: dummyResp}
	svc := NewEventCommandService(repoStub, store, nil)

	req := validRequest()
	req.ImageObjectKeys = []string{validKey}

	_, err := svc.Create(context.Background(), testProfileID, req)
	_ = assertValidationError(t, err)
}

func TestEventCommandServiceCreate_EXIFStrip(t *testing.T) {
	dummyResp := model.CreateEventResponse{ID: "event-001", CreatedAt: time.Now().UTC()}
	validKey := "natueve/tmp/" + testProfileID + "/uuid.jpg"
	jpegData := loadTestdata(t, "sample.jpg")

	store := &fakeObjectStore{
		headSize:        int64(len(jpegData)),
		headContentType: "image/jpeg",
		getData:         jpegData,
	}
	repoStub := &stubEventRepository{createResult: dummyResp}
	svc := NewEventCommandService(repoStub, store, nil)

	req := validRequest()
	req.ImageObjectKeys = []string{validKey}

	_, err := svc.Create(context.Background(), testProfileID, req)
	assertNoErr(t, err)

	// 再エンコード後のバイト列が Put に渡されていること
	if len(store.putBody) == 0 {
		t.Error("再エンコード後の画像が Put に渡されていません")
	}

	// 再エンコード後のキーが natueve/events/images/ prefix を持つこと
	if !strings.HasPrefix(store.putKey, "natueve/events/images/") {
		t.Errorf("最終キー prefix: got %q, want natueve/events/images/", store.putKey)
	}
}

func TestEventCommandServiceCreate_CompensationDelete(t *testing.T) {
	validKey := "natueve/tmp/" + testProfileID + "/uuid.jpg"
	jpegData := loadTestdata(t, "sample.jpg")

	store := &fakeObjectStore{
		headSize:        int64(len(jpegData)),
		headContentType: "image/jpeg",
		getData:         jpegData,
	}
	// repo.Create がエラーを返すように設定
	repoStub := &stubEventRepository{
		createErr: errors.New("db error"),
	}
	svc := NewEventCommandService(repoStub, store, nil)

	req := validRequest()
	req.ImageObjectKeys = []string{validKey}

	_, err := svc.Create(context.Background(), testProfileID, req)
	// repo エラーが伝播すること
	if err == nil {
		t.Fatal("エラーを期待したが nil だった")
	}
	// 補償削除が呼ばれていること
	if len(store.deleteKeys) == 0 {
		t.Error("repo.Create 失敗時に補償削除が呼ばれていません")
	}
	// 削除されたキーが natueve/events/images/ prefix を持つこと
	for _, k := range store.deleteKeys {
		if !strings.HasPrefix(k, "natueve/events/images/") {
			t.Errorf("補償削除されたキーが予期しない prefix: %q", k)
		}
	}
}

func TestEventCommandServiceCreate_StoreNilWithKeys(t *testing.T) {
	repoStub := &stubEventRepository{}
	// store=nil のまま画像キーを渡す → ValidationError
	svc := NewEventCommandService(repoStub, nil, nil)

	req := validRequest()
	req.ImageObjectKeys = []string{"natueve/tmp/" + testProfileID + "/uuid.jpg"}

	_, err := svc.Create(context.Background(), testProfileID, req)
	_ = assertValidationError(t, err)
}

// validCancelEventRequest は正常系テスト用の最小限の有効なキャンセルリクエスト。
func validCancelEventRequest() model.CancelEventRequest {
	return model.CancelEventRequest{
		Subject: "【重要】イベント開催中止のお知らせ",
		Body:    "台風接近に伴い、安全のため本イベントは中止とさせていただきます。",
	}
}

func TestEventCommandServiceCancel(t *testing.T) {
	eventUID := uuid.MustParse("a1b2c3d4-e5f6-7890-abcd-ef1234567890")
	ownerUID := uuid.MustParse("b2c3d4e5-f6a8-8901-bcde-f23456789013")
	otherUID := uuid.MustParse("c3d4e5f6-a7b8-9012-cdef-345678901234")
	cancelledAt := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name             string
		profileID        string
		eventID          string
		req              model.CancelEventRequest
		repo             *stubEventRepository
		wantForbiddenErr bool
		wantValErr       bool
		wantErr          bool
		wantErrIs        error
		wantCancelledAt  time.Time
		// wantSubject/wantBody: repo.CancelWithNotification に渡った最終的な subject/body。
		wantSubject string
		wantBody    string
		// wantTitleCalled: repo.GetTitle が呼ばれたかどうか（body 補完が必要な場合のみ true）。
		wantTitleCalled bool
	}{
		{
			name:            "正常: 主催者がキャンセル（subject/body 両方指定）",
			profileID:       ownerUID.String(),
			eventID:         eventUID.String(),
			req:             validCancelEventRequest(),
			repo:            &stubEventRepository{ownerProfileID: ownerUID.String(), cancelResult: cancelledAt},
			wantCancelledAt: cancelledAt,
			wantSubject:     "【重要】イベント開催中止のお知らせ",
			wantBody:        "台風接近に伴い、安全のため本イベントは中止とさせていただきます。",
			wantTitleCalled: false,
		},
		{
			name:      "正常: subject のみ空 → 既定件名が使われ、本文は指定値",
			profileID: ownerUID.String(),
			eventID:   eventUID.String(),
			req:       model.CancelEventRequest{Subject: "", Body: "本文"},
			repo:      &stubEventRepository{ownerProfileID: ownerUID.String(), cancelResult: cancelledAt},

			wantCancelledAt: cancelledAt,
			wantSubject:     defaultCancelSubject,
			wantBody:        "本文",
			wantTitleCalled: false,
		},
		{
			name:      "正常: body のみ空 → 件名は指定値、本文が既定文面（GetTitle が呼ばれる）",
			profileID: ownerUID.String(),
			eventID:   eventUID.String(),
			req:       model.CancelEventRequest{Subject: "件名", Body: ""},
			repo: &stubEventRepository{
				ownerProfileID: ownerUID.String(),
				cancelResult:   cancelledAt,
				title:          "サクラ観察会",
			},
			wantCancelledAt: cancelledAt,
			wantSubject:     "件名",
			wantBody:        defaultCancelBodyPrefix + "サクラ観察会",
			wantTitleCalled: true,
		},
		{
			name:      "正常: subject/body 両方空 → 既定件名＋既定本文（GetTitle が呼ばれる）",
			profileID: ownerUID.String(),
			eventID:   eventUID.String(),
			req:       model.CancelEventRequest{Subject: "", Body: ""},
			repo: &stubEventRepository{
				ownerProfileID: ownerUID.String(),
				cancelResult:   cancelledAt,
				title:          "サクラ観察会",
			},
			wantCancelledAt: cancelledAt,
			wantSubject:     defaultCancelSubject,
			wantBody:        defaultCancelBodyPrefix + "サクラ観察会",
			wantTitleCalled: true,
		},
		{
			name:      "異常: subject が上限文字数を超える場合 ValidationError",
			profileID: ownerUID.String(),
			eventID:   eventUID.String(),
			req: model.CancelEventRequest{
				Subject: strings.Repeat("あ", 256),
				Body:    "本文",
			},
			repo:       &stubEventRepository{ownerProfileID: ownerUID.String(), cancelResult: cancelledAt},
			wantValErr: true,
		},
		{
			name:      "異常: body が上限文字数を超える場合 ValidationError",
			profileID: ownerUID.String(),
			eventID:   eventUID.String(),
			req: model.CancelEventRequest{
				Subject: "件名",
				Body:    strings.Repeat("あ", 10001),
			},
			repo:       &stubEventRepository{ownerProfileID: ownerUID.String(), cancelResult: cancelledAt},
			wantValErr: true,
		},
		{
			name:             "異常: 主催者以外 → ForbiddenError",
			profileID:        otherUID.String(),
			eventID:          eventUID.String(),
			req:              validCancelEventRequest(),
			repo:             &stubEventRepository{ownerProfileID: ownerUID.String()},
			wantForbiddenErr: true,
		},
		{
			name:       "異常: eventID が不正な形式 → ValidationError",
			profileID:  ownerUID.String(),
			eventID:    "not-a-uuid",
			req:        validCancelEventRequest(),
			repo:       &stubEventRepository{ownerProfileID: ownerUID.String()},
			wantValErr: true,
		},
		{
			name:       "異常: イベントが存在しない → ValidationError",
			profileID:  ownerUID.String(),
			eventID:    eventUID.String(),
			req:        validCancelEventRequest(),
			repo:       &stubEventRepository{ownerProfileIDErr: fmt.Errorf("event %s: %w", eventUID, repository.ErrEventNotFound)},
			wantValErr: true,
		},
		{
			name:      "異常: body 省略時に GetTitle がイベント不存在を返す → ValidationError",
			profileID: ownerUID.String(),
			eventID:   eventUID.String(),
			req:       model.CancelEventRequest{Subject: "件名", Body: ""},
			repo: &stubEventRepository{
				ownerProfileID: ownerUID.String(),
				titleErr:       fmt.Errorf("event %s: %w", eventUID, repository.ErrEventNotFound),
			},
			wantValErr: true,
		},
		{
			name:      "異常: body 省略時に GetTitle が想定外のエラー → エラー伝播",
			profileID: ownerUID.String(),
			eventID:   eventUID.String(),
			req:       model.CancelEventRequest{Subject: "件名", Body: ""},
			repo: &stubEventRepository{
				ownerProfileID: ownerUID.String(),
				titleErr:       errors.New("db error"),
			},
			wantErr: true,
		},
		{
			name:      "異常: 既にキャンセル済み → ErrEventAlreadyCancelled",
			profileID: ownerUID.String(),
			eventID:   eventUID.String(),
			req:       validCancelEventRequest(),
			repo: &stubEventRepository{
				ownerProfileID: ownerUID.String(),
				cancelErr:      fmt.Errorf("event %s: %w", eventUID, repository.ErrEventAlreadyCancelled),
			},
			wantErr:   true,
			wantErrIs: repository.ErrEventAlreadyCancelled,
		},
		{
			name:      "異常: repo.CancelWithNotification が想定外のエラー → エラー伝播",
			profileID: ownerUID.String(),
			eventID:   eventUID.String(),
			req:       validCancelEventRequest(),
			repo:      &stubEventRepository{ownerProfileID: ownerUID.String(), cancelErr: errors.New("db error")},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var wakeCalled bool
			svc := NewEventCommandService(tt.repo, nil, func() { wakeCalled = true })

			resp, err := svc.Cancel(context.Background(), tt.profileID, tt.eventID, tt.req)

			switch {
			case tt.wantForbiddenErr:
				_ = assertForbiddenError(t, err)
				if wakeCalled {
					t.Error("エラー時に wake が呼ばれるべきではない")
				}
				return
			case tt.wantValErr:
				_ = assertValidationError(t, err)
				if wakeCalled {
					t.Error("エラー時に wake が呼ばれるべきではない")
				}
				return
			case tt.wantErr:
				if err == nil {
					t.Fatal("エラーを期待したが nil だった")
				}
				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Errorf("errors.Is(err, %v) = false, err = %v", tt.wantErrIs, err)
				}
				if wakeCalled {
					t.Error("エラー時に wake が呼ばれるべきではない")
				}
				return
			}

			assertNoErr(t, err)
			if resp.ID != tt.eventID {
				t.Errorf("ID: got %q, want %q", resp.ID, tt.eventID)
			}
			if !resp.CancelledAt.Equal(tt.wantCancelledAt) {
				t.Errorf("CancelledAt: got %v, want %v", resp.CancelledAt, tt.wantCancelledAt)
			}
			if tt.repo.gotCancelSubject != tt.wantSubject {
				t.Errorf("CancelWithNotification に渡った subject: got %q, want %q", tt.repo.gotCancelSubject, tt.wantSubject)
			}
			if tt.repo.gotCancelBody != tt.wantBody {
				t.Errorf("CancelWithNotification に渡った body: got %q, want %q", tt.repo.gotCancelBody, tt.wantBody)
			}
			if tt.repo.getTitleCalled != tt.wantTitleCalled {
				t.Errorf("GetTitle 呼び出し有無: got %v, want %v", tt.repo.getTitleCalled, tt.wantTitleCalled)
			}
			if !wakeCalled {
				t.Error("正常系では wake が呼ばれるべき")
			}
		})
	}
}
