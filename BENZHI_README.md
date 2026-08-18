# quorumforge-root-ceremony

本 Git 项目来自模型完成任务后的 workspace，不包含嵌套 .git 记录或本地构建产物。

## 本地构建与测试

```bash
go mod download
go build ./...
go build -o ./quorumforge ./cmd/quorumforge
go test ./...
./run_benzhi_smoke.sh
```

## Docker 构建与运行

```bash
./build_benzhi_docker.sh quorumforge-root-ceremony linux/arm64
docker run --rm -it --platform linux/arm64 quorumforge-root-ceremony:latest
./build_benzhi_docker.sh quorumforge-root-ceremony linux/amd64
docker run --rm -it --platform linux/amd64 quorumforge-root-ceremony:latest
```

构建脚本第二个参数为目标平台，必须分别完成 linux/arm64 和 linux/amd64 构建与容器验证；未提供时按照规范默认使用 linux/amd64。Dockerfile 不写死 CPU 架构，平台由脚本参数传入。
