# NatuEve API 開発用タスク
# 使い方: `make` または `make help` で一覧を表示

# swag は go.mod の swaggo/swag と同じバージョンに固定する（生成物のブレを防ぐ）。
SWAG_VERSION := v1.16.6
SWAG ?= $(shell go env GOPATH)/bin/swag
SWAG_ENTRY := cmd/api/main.go
SWAG_OUT := ./api

# golangci-lint も CI と揃えるためバージョンを固定する。
GOLANGCI_VERSION := v2.12.2
GOLANGCI ?= $(shell go env GOPATH)/bin/golangci-lint

# goose(マイグレーション CLI)はバージョンを固定し、api コンテナ内で go run する。
GOOSE_VERSION := v3.27.1
MIGRATIONS_DIR := db/migrations
# マイグレーションは api コンテナ内で実行する(DB 接続先はコンテナの環境変数 DATABASE_URL = db:5432)。
# CI など compose を使わない環境では `GOOSE_EXEC='sh -c'` を渡してホストで実行する
# (その場合 DATABASE_URL はシェルの環境変数から取得する)。
GOOSE_EXEC ?= docker compose exec -T api sh -c
GOOSE := go run github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION) -dir $(MIGRATIONS_DIR)

# ローカル開発では .env を読み込み、DATABASE_URL などを Make の変数として取り込む。
# (CI など .env が無い環境では、シェルの環境変数がそのまま使われる)
ifneq (,$(wildcard .env))
include .env
export
endif

.DEFAULT_GOAL := help

.PHONY: help
help: ## このヘルプを表示
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "} {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: setup
setup: ## 初期セットアップ (.env 作成・依存取得・CLI 一括導入)
	@test -f .env || (cp .env.example .env && echo ".env を .env.example から作成しました")
	go mod download
	$(MAKE) swag-install lint-install
	@echo "セットアップ完了。'make up' で開発用コンテナを起動できます"

.PHONY: run
run: ## ローカルでサーバを起動 (go run)
	go run ./cmd/api

.PHONY: build
build: ## ビルド確認
	go build ./...

.PHONY: test
test: ## テスト実行
	go test ./...

.PHONY: tidy
tidy: ## 依存の整理
	go mod tidy

.PHONY: fmt
fmt: ## フォーマット
	go fmt ./...

.PHONY: vet
vet: ## go vet
	go vet ./...

.PHONY: lint-install
lint-install: ## golangci-lint をインストール (バージョン固定)
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b $(shell go env GOPATH)/bin $(GOLANGCI_VERSION)

.PHONY: lint
lint: ## golangci-lint を実行
	$(GOLANGCI) run ./...

.PHONY: swag-install
swag-install: ## swag CLI をインストール (バージョン固定)
	go install github.com/swaggo/swag/cmd/swag@$(SWAG_VERSION)

.PHONY: swag
swag: ## OpenAPI ドキュメントを生成 (api/)
	$(SWAG) init -g $(SWAG_ENTRY) -o $(SWAG_OUT) --parseDependency --parseInternal

.PHONY: swag-check
swag-check: swag ## api/ が最新か確認 (CI 用: 差分があれば失敗)
	@git diff --exit-code $(SWAG_OUT) || (echo "api/ が古いです。'make swag' を実行してコミットしてください" && exit 1)

.PHONY: migrate-create
migrate-create: ## マイグレーション雛形を作成 (例: make migrate-create name=create_xxx)
	@test -n "$(name)" || (echo "name を指定してください: make migrate-create name=create_xxx" && exit 1)
	$(GOOSE_EXEC) '$(GOOSE) create $(name) sql'

.PHONY: migrate-up
migrate-up: ## マイグレーションを最新まで適用
	$(GOOSE_EXEC) '$(GOOSE) postgres "$$DATABASE_URL" up'

.PHONY: migrate-down
migrate-down: ## マイグレーションを1つ戻す
	$(GOOSE_EXEC) '$(GOOSE) postgres "$$DATABASE_URL" down'

.PHONY: migrate-status
migrate-status: ## マイグレーション適用状況を表示
	$(GOOSE_EXEC) '$(GOOSE) postgres "$$DATABASE_URL" status'

.PHONY: up
up: ## 開発用コンテナを起動 (docker compose)
	docker compose up

.PHONY: down
down: ## 開発用コンテナを停止
	docker compose down
