package config

import (
	"os"
	"reflect"
	"testing"
)

func TestLoadCORSAllowOrigins(t *testing.T) {
	tests := []struct {
		name  string
		set   bool // false なら CORS_ALLOW_ORIGINS を未設定にする
		value string
		want  []string
	}{
		{name: "未設定なら既定値", set: false, want: []string{"http://localhost:3000", "http://localhost:8787"}},
		{name: "空文字なら既定値", set: true, value: "", want: []string{"http://localhost:3000", "http://localhost:8787"}},
		{name: "単一", set: true, value: "https://example.com", want: []string{"https://example.com"}},
		{
			name: "複数", set: true, value: "http://localhost:3000,https://example.com",
			want: []string{"http://localhost:3000", "https://example.com"},
		},
		{
			name: "前後の空白を除く", set: true, value: " https://a.example.com ,\thttps://b.example.com  ",
			want: []string{"https://a.example.com", "https://b.example.com"},
		},
		{name: "空要素を除く", set: true, value: "a, ,b,", want: []string{"a", "b"}},
		{name: "空要素のみなら空スライス", set: true, value: " , ", want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// t.Setenv で元の値の復元を登録してから、未設定ケースは Unsetenv する。
			t.Setenv("CORS_ALLOW_ORIGINS", tt.value)
			if !tt.set {
				if err := os.Unsetenv("CORS_ALLOW_ORIGINS"); err != nil {
					t.Fatalf("Unsetenv failed: %v", err)
				}
			}

			got := Load().CORSAllowOrigins
			if len(got) != len(tt.want) || (len(tt.want) > 0 && !reflect.DeepEqual(got, tt.want)) {
				t.Errorf("CORSAllowOrigins = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestLoadOutboxSettings(t *testing.T) {
	t.Run("OUTBOX_WORKER_ENABLED", func(t *testing.T) {
		tests := []struct {
			name  string
			set   bool
			value string
			want  bool
		}{
			{name: "未設定なら無効（本番の既定）", set: false, want: false},
			{name: "空文字なら無効", set: true, value: "", want: false},
			{name: "true なら有効", set: true, value: "true", want: true},
			{name: "1 なら有効", set: true, value: "1", want: true},
			{name: "false なら無効", set: true, value: "false", want: false},
			{name: "解釈できない値は無効", set: true, value: "yes please", want: false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Setenv("OUTBOX_WORKER_ENABLED", tt.value)
				if !tt.set {
					if err := os.Unsetenv("OUTBOX_WORKER_ENABLED"); err != nil {
						t.Fatalf("Unsetenv failed: %v", err)
					}
				}
				if got := Load().OutboxWorkerEnabled; got != tt.want {
					t.Errorf("OutboxWorkerEnabled = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("Scheduler の認証設定を読み取る", func(t *testing.T) {
		t.Setenv("SCHEDULER_OIDC_AUDIENCE", "https://api.example.com")
		t.Setenv("SCHEDULER_SERVICE_ACCOUNT_EMAIL", "scheduler@project.iam.gserviceaccount.com")

		cfg := Load()
		if cfg.SchedulerOIDCAudience != "https://api.example.com" {
			t.Errorf("SchedulerOIDCAudience = %q", cfg.SchedulerOIDCAudience)
		}
		if cfg.SchedulerServiceAccountEmail != "scheduler@project.iam.gserviceaccount.com" {
			t.Errorf("SchedulerServiceAccountEmail = %q", cfg.SchedulerServiceAccountEmail)
		}
	})
}

func TestLoadRelaySettings(t *testing.T) {
	tests := []struct {
		name         string
		secret       string
		previous     string
		header       string
		wantSecret   string
		wantPrevious string
		wantHeader   string
	}{
		{name: "未設定", wantSecret: "", wantPrevious: "", wantHeader: ""},
		{
			name:       "値を読み込む",
			secret:     "new-secret",
			previous:   "old-secret",
			header:     "X-NatuEve-Client-IP",
			wantSecret: "new-secret", wantPrevious: "old-secret", wantHeader: "X-NatuEve-Client-IP",
		},
		{
			name:       "前後の空白・改行を除く",
			secret:     "  new-secret\n",
			previous:   "\told-secret \r\n",
			header:     " X-NatuEve-Client-IP\n",
			wantSecret: "new-secret", wantPrevious: "old-secret", wantHeader: "X-NatuEve-Client-IP",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("RELAY_SECRET", tt.secret)
			t.Setenv("RELAY_SECRET_PREVIOUS", tt.previous)
			t.Setenv("RELAY_CLIENT_IP_HEADER", tt.header)

			cfg := Load()
			if cfg.RelaySecret != tt.wantSecret {
				t.Errorf("RelaySecret = %q, want %q", cfg.RelaySecret, tt.wantSecret)
			}
			if cfg.RelaySecretPrevious != tt.wantPrevious {
				t.Errorf("RelaySecretPrevious = %q, want %q", cfg.RelaySecretPrevious, tt.wantPrevious)
			}
			if cfg.RelayClientIPHeader != tt.wantHeader {
				t.Errorf("RelayClientIPHeader = %q, want %q", cfg.RelayClientIPHeader, tt.wantHeader)
			}
		})
	}
}
