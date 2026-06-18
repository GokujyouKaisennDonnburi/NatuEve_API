# NatuIve API

なちゅいべのバックエンド。Web・モバイルから呼び出される API サーバー（Go / Gin 製）。

## ドキュメント

- [環境構築・起動手順（Tech.md）](docs/Tech.md)
- [開発ガイド：規約・構成・注意点（Development.md）](docs/Development.md)
- [依存ライブラリ（dependencies.md）](docs/dependencies.md)

## クイックスタート

```bash
cp .env.example .env
make run          # ローカル起動（または docker compose は make up）
make help         # タスク一覧
```

API ドキュメント（Swagger UI）: 起動後 `http://localhost:8080/swagger/index.html`
