# ---- build stage ----
FROM golang:1.26.4-alpine AS build

WORKDIR /app

# 依存だけ先にダウンロードしてレイヤーキャッシュを効かせる
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# 静的バイナリをビルド（distroless で動かすため CGO 無効）
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /server ./cmd/api

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /
COPY --from=build /server /server

EXPOSE 8080
USER nonroot:nonroot

ENTRYPOINT ["/server"]
