# deploy-server 构建入口
#
# 交付物是**一个可执行文件**：前端静态资源通过 go:embed 打进二进制。
# 因此正确顺序是「先构建前端 → 拷进 embed 目录 → 再编译 Go」。

VERSION ?= 0.1.0
LDFLAGS := -s -w -X main.version=$(VERSION)
BIN := bin

.PHONY: help build ui all test vet fmt linux linux-arm64 docker clean run

help:
	@echo "make ui          把 ../deploy-server-ui/dist 拷进内嵌目录（先手工 pnpm build）"
	@echo "make build       编译本机二进制"
	@echo "make all         ui + build"
	@echo "make linux       交叉编译 Linux/amd64（CGO_ENABLED=0）"
	@echo "make linux-arm64 交叉编译 Linux/arm64"
	@echo "make test        跑测试"
	@echo "make docker      构建镜像（上下文是仓库根目录）"
	@echo "make clean       清理产物"

ui:
	@test -d ../deploy-server-ui/dist || (echo "前端还没构建：cd ../deploy-server-ui && pnpm build"; exit 1)
	rm -rf internal/web/dist
	mkdir -p internal/web/dist
	cp -r ../deploy-server-ui/dist/. internal/web/dist/
	# 排除两类不该进生产的东西：
	#   *.map         source map，能占到 dist 的一半体积
	#   *hot-update*  `max dev` 往 dist/ 里写的开发态热更新产物
	find internal/web/dist \( -name '*.map' -o -name '*hot-update*' \) -delete
	@test -f internal/web/dist/index.html || (echo "embed 目录里没有 index.html，前端产物不完整"; exit 1)
	@echo "已拷入 $$(find internal/web/dist -type f | wc -l) 个前端文件"

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
	./$(BIN)/deploy-server -data-dir ./data -web-root ./workspaces -listen 127.0.0.1:8080

# 镜像构建的上下文必须是**仓库根目录**，不是 deploy-server/ ——
# Dockerfile 要同时看到 deploy-server 与 deploy-server-ui（前端要单独构建）。
docker:
	cd .. && docker build -t deploy-server:$(VERSION) -f deploy-server/Dockerfile .

clean:
	rm -rf $(BIN)
