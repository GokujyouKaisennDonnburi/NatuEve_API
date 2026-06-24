package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GokujyouKaisennDonnburi/NatuIve_API/internal/model"
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
		CreatedAt: time.Now(),
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
			name: "正常: 任意フィールドあり（Capacity・ExternalURL・Items・画像・PDF）",
			req: func() model.CreateEventRequest {
				r := validRequest()
				r.Capacity = 30
				r.ExternalURL = "https://example.com/event"
				r.Items = []model.EventItemInput{
					{Item: "双眼鏡", IsRequired: true},
				}
				r.ImageObjectKeys = []string{"images/photo1.jpg"}
				r.PdfObjectKeys = []string{"pdfs/doc1.pdf"}
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
				if len(e.ImageObjectKeys) != 1 {
					t.Errorf("ImageObjectKeys: got %v", e.ImageObjectKeys)
				}
				if len(e.PdfObjectKeys) != 1 {
					t.Errorf("PdfObjectKeys: got %v", e.PdfObjectKeys)
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
			name:    "異常: repository の Create がエラーを返す",
			req:     validRequest(),
			stubErr: errors.New("db error"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubEventRepository{
				createResult: dummyResp,
				createErr:    tt.stubErr,
			}
			svc := NewEventCommandService(stub)

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
