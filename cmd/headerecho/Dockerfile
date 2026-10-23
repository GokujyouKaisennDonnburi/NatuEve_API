# 利用者 IP の検証用サービス（cmd/headerecho）のイメージ。リポジトリ直下をビルドコンテキストにする:
#   docker build --platform linux/amd64 -f cmd/headerecho/Dockerfile -t <イメージ> .

# ---- build stage ----
FROM golang:1.26.10-alpine AS build

WORKDIR /app

# 依存だけ先にダウンロードしてレイヤーキャッシュを効かせる
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# 静的バイナリをビルド（distroless で動かすため CGO 無効）
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /headerecho ./cmd/headerecho

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /
COPY --from=build /headerecho /headerecho

EXPOSE 8080
USER nonroot:nonroot

ENTRYPOINT ["/headerecho"]
