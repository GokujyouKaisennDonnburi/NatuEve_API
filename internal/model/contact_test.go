package model

import "testing"

func TestContactCategory(t *testing.T) {
	for _, tt := range []struct {
		category ContactCategory
		label    string
	}{
		{ContactCategoryRequest, "要望"},
		{ContactCategoryBug, "不具合報告"},
		{ContactCategoryOther, "その他"},
		{"", ""},
		{"unknown", ""},
	} {
		t.Run(string(tt.category), func(t *testing.T) {
			if got := tt.category.Label(); got != tt.label {
				t.Errorf("Label()=%q want=%q", got, tt.label)
			}
			if got := tt.category.IsValid(); got != (tt.label != "") {
				t.Errorf("IsValid()=%t label=%q", got, tt.label)
			}
		})
	}
}
