# 多阶段构建：先构建前端，再构建后端，最后只留一个二进制。
#
# 用法（在**仓库根目录**执行；前端 `deploy-server-ui/` 与后端 `deploy-server/` 都在同一个仓库里）：
#
#   docker build -t deploy-server:0.1.0 -f deploy-server/Dockerfile .
#
# 也可用 make：cd deploy-server && make docker

# ---------- 前端 ----------
FROM node:22-bookworm-slim AS ui
WORKDIR /src/deploy-server-ui
RUN corepack enable
COPY deploy-server-ui/package.json deploy-server-ui/pnpm-lock.yaml deploy-server-ui/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY deploy-server-ui/ ./
# 前端仓库里带了一堆示例页与 mock，构建时不需要它们联网
RUN pnpm build

# ---------- 后端 ----------
FROM golang:1.24-bookworm AS build
WORKDIR /src/deploy-server
COPY deploy-server/go.mod deploy-server/go.sum ./
RUN go mod download
COPY deploy-server/ ./
# 用前端产物替换占位页，让 embed 打进真实 UI
RUN rm -rf internal/web/dist && mkdir -p internal/web/dist
COPY --from=ui /src/deploy-server-ui/dist/ ./internal/web/dist/
ARG VERSION=0.1.0
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" -o /out/deploy-server ./cmd/server

# ---------- 运行 ----------
FROM debian:bookworm-slim
# 自检页会检查 git/rsync/curl/bash：平台不负责安装工具链（需求 D6），
# 但一个能"开箱跑部署"的镜像至少应该有它们。
RUN apt-get update && apt-get install -y --no-install-recommends \
        git bash curl ca-certificates rsync openssh-client tzdata \
    && rm -rf /var/lib/apt/lists/*
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY --from=build /out/deploy-server /usr/local/bin/deploy-server
VOLUME ["/data", "/workspaces"]
EXPOSE 10055
# 默认只监听容器内网卡，由外部反代/端口映射暴露，避免无意间裸奔到公网。
ENTRYPOINT ["deploy-server"]
CMD ["-listen", "0.0.0.0:10055", "-data-dir", "/data", "-web-root", "/workspaces"]
