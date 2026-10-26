package service

import (
	"reflect"
	"testing"
)

func TestPublicURLResolver_URL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		key     string
		want    string
	}{
		{
			name:    "通常のキーを連結する",
			baseURL: "https://assets.natuportal.org",
			key:     "natueve/events/images/abc.jpg",
			want:    "https://assets.natuportal.org/natueve/events/images/abc.jpg",
		},
		{
			name:    "ベースURLの末尾スラッシュは正規化される",
			baseURL: "https://assets.natuportal.org/",
			key:     "natueve/events/images/abc.jpg",
			want:    "https://assets.natuportal.org/natueve/events/images/abc.jpg",
		},
		{
			name:    "キー先頭のスラッシュは重複させない",
			baseURL: "https://assets.natuportal.org",
			key:     "/natueve/events/images/abc.jpg",
			want:    "https://assets.natuportal.org/natueve/events/images/abc.jpg",
		},
		{
			name:    "ベースURL未設定なら空文字",
			baseURL: "",
			key:     "natueve/events/images/abc.jpg",
			want:    "",
		},
		{
			name:    "キーが空なら空文字",
			baseURL: "https://assets.natuportal.org",
			key:     "",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewPublicURLResolver(tt.baseURL).URL(tt.key)
			if got != tt.want {
				t.Errorf("URL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPublicURLResolver_URLs(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		keys    []string
		want    []string
	}{
		{
			name:    "複数キーを変換する",
			baseURL: "https://assets.natuportal.org",
			keys:    []string{"natueve/events/images/a.jpg", "natueve/events/images/b.png"},
			want: []string{
				"https://assets.natuportal.org/natueve/events/images/a.jpg",
				"https://assets.natuportal.org/natueve/events/images/b.png",
			},
		},
		{
			name:    "nil 入力でも空スライス（非 nil）を返す",
			baseURL: "https://assets.natuportal.org",
			keys:    nil,
			want:    []string{},
		},
		{
			name:    "ベースURL未設定なら空スライス",
			baseURL: "",
			keys:    []string{"natueve/events/images/a.jpg"},
			want:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewPublicURLResolver(tt.baseURL).URLs(tt.keys)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("URLs() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
