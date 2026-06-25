# NatuIve API

なちゅいべのバックエンド。Web・モバイルから呼び出される API サーバー（Go / Gin 製）。

## ドキュメント

- [環境構築・起動手順（Tech.md）](docs/Tech.md)
- [開発ガイド：規約・構成・注意点（Development.md）](docs/Development.md)
- [依存ライブラリ（dependencies.md）](docs/dependencies.md)

## クイックスタート

```bash
cp .env.example .env
make up      # 開発サーバ起動（Docker + Air ホットリロード。停止は Ctrl+C → make down）
make help    # その他のタスク一覧（lint / test / swag など）
```

API ドキュメント（Swagger UI）: 起動後 `http://localhost:8085/swagger/index.html`
