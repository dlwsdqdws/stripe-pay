# API 认证指南

## 概述

为了保护 API 接口不被未授权访问，系统实现了基于 API Key 的认证机制。所有支付相关接口都需要提供有效的 API Key 才能访问。

## 认证方式

### 1. API Key 认证

系统支持两种方式传递 API Key：

#### 方式一：通过 Header（推荐）
```
X-API-Key: <your-api-key>
```

#### 方式二：通过 Authorization Header
```
Authorization: Bearer <your-api-key>
```

#### 方式三：通过查询参数（不推荐，仅用于兼容）
```
?api_key=<your-api-key>
```

## 生成 API Key

### 使用工具生成

运行以下命令生成新的 API Key：

```bash
go run tools/generate_api_key.go
```

或者编译后运行：

```bash
go build -o generate_api_key tools/generate_api_key.go
./generate_api_key
```

### 手动生成

API Key 是一个 base64 编码的 32 字节随机字符串。你也可以使用其他工具生成。

## 配置 API Key

### 环境变量方式（推荐）

设置环境变量来配置 API Key：

```bash
# 普通 API Key（用于普通接口）
export API_KEYS="key1,key2,key3"

# 管理员 API Key（用于管理员接口）
export ADMIN_API_KEYS="admin_key1,admin_key2"
```

**注意：**
- 多个 API Key 用逗号分隔
- 管理员 API Key 自动拥有普通 API Key 的所有权限
- 生产环境建议使用环境变量，不要将密钥写入代码或配置文件

### 配置文件方式（未来支持）

未来版本将支持在 `config.yaml` 中配置 API Key。

## 接口权限说明

### 公开接口（无需认证）

以下接口不需要 API Key：

- `GET /ping` - 健康检查
- `GET /health` - 详细健康检查
- `GET /metrics` - Prometheus 指标
- `POST /api/v1/stripe/webhook` - Stripe Webhook（使用签名验证）
- `POST /api/v1/apple/webhook` - Apple Webhook（使用签名验证）

### 普通接口（需要 API Key）

以下接口需要有效的 API Key：

- `GET /api/v1/pricing` - 获取定价信息
- `POST /api/v1/stripe/create-payment` - 创建支付
- `POST /api/v1/stripe/create-wechat-payment` - 创建微信支付
- `POST /api/v1/stripe/confirm-payment` - 确认支付
- `POST /api/v1/stripe/refund` - 退款
- `POST /api/v1/apple/verify` - 验证 Apple 内购
- `POST /api/v1/apple/verify-subscription` - 验证 Apple 订阅
- `GET /api/v1/user/:user_id/payment-info` - 获取用户支付信息
- `GET /api/v1/user/:user_id/payment-history` - 获取用户支付历史
- `POST /api/v1/payment/update-status` - 更新支付状态
- `GET /api/v1/payment/status/:id` - 查询支付状态
- `GET /api/v1/payment/status-change/:payment_intent_id` - 查询状态变化

### 管理员接口（需要管理员 API Key）

以下接口需要管理员 API Key：

- `GET /api/v1/payment/config` - 获取支付配置
- `PUT /api/v1/payment/config` - 更新支付配置

## 使用示例

### cURL 示例

```bash
# 使用 X-API-Key Header
curl -X POST http://localhost:8080/api/v1/stripe/create-payment \
  -H "X-API-Key: your-api-key-here" \
  -H "Content-Type: application/json" \
  -d '{"user_id": "user123", "description": "Test payment"}'

# 使用 Authorization Header
curl -X POST http://localhost:8080/api/v1/stripe/create-payment \
  -H "Authorization: Bearer your-api-key-here" \
  -H "Content-Type: application/json" \
  -d '{"user_id": "user123", "description": "Test payment"}'

# 管理员接口
curl -X GET http://localhost:8080/api/v1/payment/config \
  -H "X-API-Key: your-admin-api-key-here"
```

### JavaScript/TypeScript 示例

```javascript
// 使用 fetch API
fetch('http://localhost:8080/api/v1/stripe/create-payment', {
  method: 'POST',
  headers: {
    'X-API-Key': 'your-api-key-here',
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({
    user_id: 'user123',
    description: 'Test payment'
  })
})
.then(response => response.json())
.then(data => console.log(data));
```

### Python 示例

```python
import requests

headers = {
    'X-API-Key': 'your-api-key-here',
    'Content-Type': 'application/json'
}

data = {
    'user_id': 'user123',
    'description': 'Test payment'
}

response = requests.post(
    'http://localhost:8080/api/v1/stripe/create-payment',
    headers=headers,
    json=data
)

print(response.json())
```

## 错误响应

### 缺少 API Key

```json
{
  "code": 401,
  "message": "Unauthorized",
  "details": "API key is required. Please provide X-API-Key header or Authorization: Bearer <api_key>",
  "error_id": "ERR-..."
}
```

### 无效的 API Key

```json
{
  "code": 401,
  "message": "Unauthorized",
  "details": "Invalid API key",
  "error_id": "ERR-..."
}
```

### 权限不足（管理员接口）

```json
{
  "code": 403,
  "message": "Forbidden",
  "details": "Admin access required",
  "error_id": "ERR-..."
}
```

## 安全建议

1. **保护 API Key**：不要将 API Key 提交到代码仓库、日志或公开文档中
2. **使用环境变量**：生产环境使用环境变量存储 API Key
3. **定期轮换**：定期更换 API Key，特别是怀疑泄露时
4. **最小权限原则**：只为需要的接口分配最小权限的 API Key
5. **HTTPS**：生产环境必须使用 HTTPS 传输 API Key
6. **监控异常**：监控 API Key 的使用情况，发现异常及时处理

## 禁用认证（开发环境）

如果需要临时禁用认证（仅用于开发测试），可以设置环境变量：

```bash
export AUTH_ENABLED=false
```

或者在代码中修改 `common/auth.go` 中的 `defaultAuthConfig.Enabled = false`。

**警告：生产环境必须启用认证！**

## 故障排查

### 问题：认证失败，但 API Key 正确

1. 检查环境变量是否正确设置
2. 检查 API Key 是否包含多余的空格或换行符
3. 检查服务器日志，查看具体的错误信息
4. 确认 API Key 是否在允许的列表中

### 问题：管理员接口返回 403

1. 确认使用的是管理员 API Key，不是普通 API Key
2. 检查 `ADMIN_API_KEYS` 环境变量是否正确设置
3. 确认管理员 API Key 在允许的列表中

## 更新日志

- v1.0.0: 初始版本，支持 API Key 认证和管理员权限

