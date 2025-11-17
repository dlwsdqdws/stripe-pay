# Pay API

一个支持 Stripe 支付和 Apple 内购的 Go 后端服务。

## 功能特性

- ✅ Stripe 支付集成
- ✅ Apple App Store 内购验证
- ✅ Webhook 支持
- ✅ RESTful API
- ✅ 容器化部署支持

## 快速开始

### 环境要求

- Go 1.21+
- Docker & Docker Compose（可选）

### 本地开发

1. 安装依赖
```bash
go mod download
```

2. 配置环境变量
```bash
cp .env.example .env
# 编辑 .env 文件，填入你的 Stripe 和 Apple 配置
```

3. 启动服务
```bash
go run main.go
```

服务将在 `http://localhost:8080` 启动

### Docker 部署

```bash
docker-compose up -d
```

## API 文档

### 健康检查

```bash
GET /ping
```

### Stripe 支付

#### 创建支付

```bash
POST /api/v1/stripe/create-payment
Content-Type: application/json

{
  "amount": 1000,
  "currency": "usd",
  "description": "Test payment"
}
```

响应：
```json
{
  "client_secret": "pi_xxx_secret_xxx",
  "payment_id": "uuid"
}
```

#### 确认支付

```bash
POST /api/v1/stripe/confirm-payment
Content-Type: application/json

{
  "payment_id": "uuid"
}
```

#### Webhook

```bash
POST /api/v1/stripe/webhook
```

### Apple 内购

#### 验证购买

```bash
POST /api/v1/apple/verify
Content-Type: application/json

{
  "receipt_data": "base64_receipt_data",
  "password": "optional_shared_secret"
}
```

#### 验证订阅

```bash
POST /api/v1/apple/verify-subscription
Content-Type: application/json

{
  "receipt_data": "base64_receipt_data"
}
```

### 查询支付状态

```bash
GET /api/v1/payment/status/:id
```

## 配置说明

### config.yaml

```yaml
server:
  port: 8080
  host: 0.0.0.0

stripe:
  secret_key: ""  # Stripe 密钥
  webhook_secret: ""  # Webhook 密钥

apple:
  shared_secret: ""  # App Store 共享密钥
  production_url: "https://buy.itunes.apple.com/verifyReceipt"
  sandbox_url: "https://sandbox.itunes.apple.com/verifyReceipt"

log:
  level: "info"
```

### 环境变量

- `STRIPE_SECRET_KEY`: Stripe Secret Key
- `STRIPE_WEBHOOK_SECRET`: Stripe Webhook Secret
- `APPLE_SHARED_SECRET`: Apple Shared Secret

## 开发

### 项目结构

```
pay-api/
├── main.go           # 应用入口
├── config.yaml       # 配置文件
├── conf/             # 配置管理
│   └── config.go
├── biz/              # 业务逻辑
│   └── payment_handler.go
├── go.mod
├── go.sum
├── Dockerfile
└── README.md
```

## 安全性

⚠️ **重要**：生产环境请务必：
- 使用 HTTPS
- 配置正确的 Webhook 签名验证
- 保护敏感配置信息
- 实现适当的认证和授权
- 使用数据库存储支付状态（而不是内存）

## 许可证

MIT