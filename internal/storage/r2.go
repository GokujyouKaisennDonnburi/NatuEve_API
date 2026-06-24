// Package storage は Cloudflare R2 への S3 互換 API アクセスを提供する。
package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// R2Client は Cloudflare R2 の S3 互換 API クライアント。
type R2Client struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
}

// NewR2Client は R2 用に初期化された R2Client を返す。
//
// accountID: Cloudflare アカウント ID（エンドポイント URL の構成に使う）
// accessKeyID / secretAccessKey: R2 の S3 互換 API 認証情報
// bucket: 対象バケット名
func NewR2Client(accountID, accessKeyID, secretAccessKey, bucket string) *R2Client {
	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", accountID)

	client := s3.New(s3.Options{
		BaseEndpoint: aws.String(endpoint),
		Region:       "auto",
		Credentials:  credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""),
		UsePathStyle: true,
	})

	return &R2Client{
		client:  client,
		presign: s3.NewPresignClient(client),
		bucket:  bucket,
	}
}

// PresignPut は指定キー・Content-Type での PUT を署名付き URL として発行する。
//
// 有効期限は 5 分。Content-Type が署名に含まれるため、アップロード時に
// 同じ Content-Type を指定しないと署名検証エラーになる。
func (r *R2Client) PresignPut(ctx context.Context, key, contentType string) (url string, expiresAt time.Time, err error) {
	const ttl = 5 * time.Minute

	req, err := r.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(r.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, func(o *s3.PresignOptions) {
		o.Expires = ttl
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("presign put object: %w", err)
	}
	return req.URL, time.Now().UTC().Add(ttl), nil
}

// Head はオブジェクトのメタデータ（サイズ・Content-Type）を返す。
//
// オブジェクトが存在しない場合はエラーを返す。
func (r *R2Client) Head(ctx context.Context, key string) (size int64, contentType string, err error) {
	out, err := r.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, "", fmt.Errorf("head object %q: %w", key, err)
	}
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	if out.ContentType != nil {
		contentType = *out.ContentType
	}
	return size, contentType, nil
}

// Get はオブジェクトの全バイトを返す。
//
// maxBytes を超えるオブジェクトは Head→Get 間の差し替え（TOCTOU）による
// 巨大ファイルのメモリ展開を防ぐために io.LimitReader で上限を強制する。
// 読み取りバイト数が maxBytes を超えた場合はエラーを返す。
func (r *R2Client) Get(ctx context.Context, key string, maxBytes int64) ([]byte, error) {
	out, err := r.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get object %q: %w", key, err)
	}
	defer func() { _ = out.Body.Close() }()

	// maxBytes+1 まで読み、実際に maxBytes を超えたらエラーにする。
	limited := io.LimitReader(out.Body, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read object body %q: %w", key, err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("get object %q: サイズが上限 %d bytes を超えています", key, maxBytes)
	}
	return data, nil
}

// Put は body を指定キー・Content-Type で PUT する。
func (r *R2Client) Put(ctx context.Context, key string, body []byte, contentType string) error {
	_, err := r.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(r.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("put object %q: %w", key, err)
	}
	return nil
}

// Copy は R2 バケット内でオブジェクトをコピーする。
func (r *R2Client) Copy(ctx context.Context, srcKey, dstKey string) error {
	copySource := fmt.Sprintf("%s/%s", r.bucket, srcKey)
	_, err := r.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(r.bucket),
		CopySource: aws.String(copySource),
		Key:        aws.String(dstKey),
	})
	if err != nil {
		return fmt.Errorf("copy object %q -> %q: %w", srcKey, dstKey, err)
	}
	return nil
}

// Delete はオブジェクトを削除する。オブジェクトが存在しない場合もエラーにしない。
func (r *R2Client) Delete(ctx context.Context, key string) error {
	_, err := r.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(r.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("delete object %q: %w", key, err)
	}
	return nil
}
