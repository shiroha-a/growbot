# syntax=docker/dockerfile:1

# ---- build stage ----
FROM golang:1.26.3-bookworm AS build
WORKDIR /src

# 依存を先に取得してレイヤーキャッシュを効かせる。
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
# 純Go(modernc.org/sqlite は cgo 不要、kagome は辞書を埋め込み)なので CGO 無効で
# 静的バイナリをビルドする。これにより distroless static で動かせる。
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/growbot ./cmd/bot

# 永続化用データディレクトリを nonroot(uid 65532)所有で用意する。
# (.keep を置くことで空ディレクトリでも確実に COPY される)
RUN mkdir -p /out/data && touch /out/data/.keep && chown -R 65532:65532 /out/data

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot
# distroless/static には HTTPS 用の ca-certificates が同梱。tzdata はバイナリに埋め込み済み。
COPY --from=build /out/growbot /growbot
COPY --from=build --chown=65532:65532 /out/data /data

# 既定の DB 保存先。永続化のため /data に volume をマウントする。
ENV DB_PATH=/data/bot.db
VOLUME ["/data"]

ENTRYPOINT ["/growbot"]
