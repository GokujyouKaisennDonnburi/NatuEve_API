package model

import "time"

// PresignRequest は presign エンドポイントへのリクエスト DTO。
//
//	@Description	アップロード用署名付き URL の発行リクエスト。
type PresignRequest struct {
	// Kind はアップロードするファイルの種別。"image" または "pdf" のみ有効。
	Kind string `json:"kind" example:"image" binding:"required,oneof=image pdf"`
	// ContentType は MIME タイプ。
	// 画像: "image/jpeg" / "image/png"
	// PDF: "application/pdf"
	ContentType string `json:"contentType" example:"image/jpeg" binding:"required"`
	// Size はアップロードするファイルのサイズ（バイト）
	// image は最大10MiB、pdf は最大20MiB
	Size int64 `json:"size" example:"10485760" binding:"required,gt=0"`
}

// PresignResponse は presign エンドポイントのレスポンス DTO。
//
//	@Description	アップロード用署名付き URL とオブジェクトキー。
type PresignResponse struct {
	// UploadURL はファイルを直接 PUT するための署名付き URL。
	UploadURL string `json:"uploadUrl" example:"https://xxxxx.r2.cloudflarestorage.com/natuportal/natueve/tmp/profile-uuid/uuid.jpg?X-Amz-..."`
	// ObjectKey はアップロード先のオブジェクトキー。イベント作成時に imageObjectKeys/pdfObjectKeys に渡す。
	ObjectKey string `json:"objectKey" example:"natueve/tmp/profile-uuid/uuid.jpg"`
	// ExpiresAt は署名付き URL の有効期限(RFC3339)。
	ExpiresAt time.Time `json:"expiresAt" example:"2026-07-01T10:05:00Z"`
}
