# deploy-server 构建入口
#
# 交付物是**一个可执行文件**：前端静态资源通过 go:embed 打进二进制。
# 因此正确顺序是「先构建前端 → 拷进 embed 目录 → 再编译 Go」。

VERSION ?= 0.1.0
LDFLAGS := -s -w -X main.version=$(VERSION)
BIN := bin

# 前端目录 deploy-server-ui 的位置。
#
# 它在同一个仓库里、与 deploy-server 同级，所以 ../deploy-server-ui 就能命中；
# 但为了不因为调整目录层级而失效，这里按候选顺序探测而不是写死一条路径。
# 要覆盖就 `make ui UI_DIR=/path/to/deploy-server-ui`。
UI_DIR ?= $(shell for d in ../deploy-server-ui ../../deploy-server-ui deploy-server-ui ../../../deploy-server-ui; do \
	if [ -f "$$d/package.json" ]; then echo "$$d"; break; fi; done)

.PHONY: help build ui all test vet fmt linux linux-arm64 docker clean run

help:
	@echo "make ui          把 \$$(UI_DIR)/dist 拷进内嵌目录（先手工 pnpm build）"
	@echo "                 当前探测到 UI_DIR = $(if $(UI_DIR),$(UI_DIR),<未找到，可用 UI_DIR=... 指定>)"
	@echo "make build       编译本机二进制"
	@echo "make all         ui + build"
	@echo "make linux       交叉编译 Linux/amd64（CGO_ENABLED=0）"
	@echo "make linux-arm64 交叉编译 Linux/arm64"
	@echo "make test        跑测试"
	@echo "make docker      构建镜像（上下文是仓库根目录）"
	@echo "make clean       清理产物"

ui:
	@test -n "$(UI_DIR)" || (echo "cannot find the frontend repo deploy-server-ui (its directory should contain package.json); run: make ui UI_DIR=<path>"; exit 1)
	@test -d $(UI_DIR)/dist || (echo "the frontend is not built yet: cd $(UI_DIR) && pnpm install && pnpm build"; exit 1)
	rm -rf internal/web/dist
	mkdir -p internal/web/dist
	cp -r $(UI_DIR)/dist/. internal/web/dist/
	# Exclude the two kinds of things that must not ship to production:
	#   *.map         source maps, they can account for half the size of dist/
	#   *hot-update*  dev-mode hot-update artifacts that `max dev` writes into dist/
	find internal/web/dist \( -name '*.map' -o -name '*hot-update*' \) -delete
	@test -f internal/web/dist/index.html || (echo "no index.html in the embed directory - the frontend build output is incomplete"; exit 1)
	@echo "copied $$(find internal/web/dist -type f | wc -l) frontend files from $(UI_DIR)"

build:
	@mkdir -p $(BIN)
	go build -ldflags "$(LDFLAGS)" -o $(BIN)/deploy-server ./cmd/server

all: ui build

linux:
	@mkdir -p $(BIN)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/deploy-server-linux-amd64 ./cmd/server

linux-arm64:
	@mkdir -p $(BIN)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(BIN)/deploy-server-linux-arm64 ./cmd/server

test:
	go test ./... -count=1

vet:
	go vet ./...

fmt:
	gofmt -l -w .

run: build
	./$(BIN)/deploy-server -data-dir ./data -web-root ./workspaces -listen 127.0.0.1:10055

# 镜像构建的上下文必须是**仓库根目录**（即 deploy-server 的上一级）——
# Dockerfile 要同时看到 deploy-server/ 与 deploy-server-ui/，而它们在同一个仓库里。
docker:
	cd .. && docker build -t deploy-server:$(VERSION) -f deploy-server/Dockerfile .

clean:
	rm -rf $(BIN)
