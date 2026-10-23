package db

import "testing"

func TestParseMaxOpenConns(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    int
		wantErr bool
	}{
		{name: "空なら上限なし", value: "", want: 0},
		{name: "整数を読み込む", value: "6", want: 6},
		{name: "最小値は受け付ける", value: "2", want: 2},
		{name: "最小値未満はエラー", value: "1", wantErr: true},
		{name: "0 はエラー", value: "0", wantErr: true},
		{name: "負の値はエラー", value: "-1", wantErr: true},
		{name: "整数でない値はエラー", value: "six", wantErr: true},
		{name: "小数はエラー", value: "6.5", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMaxOpenConns(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseMaxOpenConns(%q) = %d, want error", tt.value, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMaxOpenConns(%q) error = %v", tt.value, err)
			}
			if got != tt.want {
				t.Errorf("ParseMaxOpenConns(%q) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}
