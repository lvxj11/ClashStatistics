# 多阶段构建，减小最终镜像大小
FROM golang:1.25.7-alpine AS builder

# 设置工作目录
WORKDIR /app

# 复制 go.mod 和 go.sum
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
RUN go build -o clash-statistics .

# 最终镜像
FROM alpine:3.19

# 设置工作目录
WORKDIR /app

# 复制构建产物
COPY --from=builder /app/clash-statistics /app/
COPY --from=builder /app/templates /app/templates/

# 设置时区
RUN apk add --no-cache tzdata
ENV TZ=Asia/Shanghai

# 创建非 root 用户
RUN adduser -D -u 1000 appuser
USER appuser

# 暴露端口
EXPOSE 8080

# 启动命令
CMD ["/app/clash-statistics"]