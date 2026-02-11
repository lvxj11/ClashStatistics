# 多阶段构建，减小最终镜像大小
FROM golang:1.25.7-alpine AS builder

# 安装必要的 C 编译工具（用于 go-sqlite3）
RUN apk add --no-cache gcc musl-dev

# 设置工作目录
WORKDIR /app

# 复制源代码
COPY . .

# 启用 CGO 并构建应用
RUN CGO_ENABLED=1 go mod download && CGO_ENABLED=1 go build -o clash-statistics .

# 最终镜像
FROM alpine:3.23

# 设置工作目录
WORKDIR /app

# 复制构建产物
COPY --from=builder /app/clash-statistics /app/

# 设置时区、创建数据目录和非 root 用户
RUN apk add --no-cache tzdata && \
    mkdir -p /app/data && \
    chmod 755 /app/data && \
    adduser -D -u 1000 appuser && \
    chown -R appuser:appuser /app/data

ENV TZ=Asia/Shanghai

USER appuser

# 暴露端口
EXPOSE 8080

# 启动命令
CMD ["/app/clash-statistics"]