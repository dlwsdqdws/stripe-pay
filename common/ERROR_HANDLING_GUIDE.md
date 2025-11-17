# 统一错误处理使用指南

## 概述

统一错误处理机制提供了：
- ✅ 统一的错误响应格式
- ✅ 自动清理敏感信息
- ✅ 错误ID追踪
- ✅ 开发/生产环境区分
- ✅ Panic恢复机制

## 错误响应格式

### 生产环境
```json
{
    "code": 400,
    "message": "Invalid request",
    "error_id": "ERR-1234567890"
}
```

### 开发环境
```json
{
    "code": 400,
    "message": "Invalid request",
    "details": "具体错误信息（包含敏感信息）",
    "error_id": "ERR-1234567890"
}
```

## 使用方法

### 1. 使用预定义错误

```go
import "pay-api/common"

func MyHandler(ctx context.Context, c *app.RequestContext) {
    // 无效请求
    if invalid {
        common.SendError(c, common.ErrInvalidRequest)
        return
    }

    // 资源未找到
    if notFound {
        common.SendError(c, common.ErrPaymentNotFound)
        return
    }

    // 数据库错误
    if dbErr != nil {
        common.SendError(c, common.ErrDatabaseError)
        return
    }
}
```

### 2. 创建自定义错误

```go
// 方式1: 使用NewAPIError
err := common.NewAPIError(400, "Custom error message")
common.SendError(c, err)

// 方式2: 带详细信息（仅开发环境显示）
err := common.NewAPIError(400, "Custom error message").
    WithDetails("详细错误信息")
common.SendError(c, err)

// 方式3: 带错误ID
err := common.NewAPIError(400, "Custom error message").
    WithErrorID("CUSTOM-ERR-001")
common.SendError(c, err)
```

### 3. 包装内部错误（自动清理敏感信息）

```go
// 自动将内部错误转换为API错误，并清理敏感信息
if err != nil {
    common.SendError(c, err) // 自动处理
    return
}
```

### 4. 使用SendErrorWithCode（快捷方式）

```go
// 简单错误
common.SendErrorWithCode(c, 400, "Invalid parameter")

// 带详细信息
common.SendErrorWithCode(c, 500, "Internal error", "Database connection failed")
```

### 5. 成功响应

```go
// 使用SendSuccess
common.SendSuccess(c, data)

// 或使用SendData（兼容旧代码）
common.SendData(c, data)
```

## 预定义错误类型

### 客户端错误 (4xx)

| 错误 | 状态码 | 说明 |
|------|--------|------|
| `ErrInvalidRequest` | 400 | 无效请求 |
| `ErrInvalidParameter` | 400 | 无效参数 |
| `ErrMissingParameter` | 400 | 缺少必需参数 |
| `ErrValidationFailed` | 400 | 验证失败 |
| `ErrUnauthorized` | 401 | 未授权 |
| `ErrForbidden` | 403 | 禁止访问 |
| `ErrNotFound` | 404 | 资源未找到 |
| `ErrPaymentNotFound` | 404 | 支付未找到 |
| `ErrUserNotFound` | 404 | 用户未找到 |
| `ErrConflict` | 409 | 资源冲突 |
| `ErrTooManyRequests` | 429 | 请求过多 |

### 服务器错误 (5xx)

| 错误 | 状态码 | 说明 |
|------|--------|------|
| `ErrInternalServer` | 500 | 内部服务器错误 |
| `ErrDatabaseError` | 500 | 数据库错误 |
| `ErrExternalService` | 500 | 外部服务错误 |
| `ErrPaymentProcessing` | 500 | 支付处理错误 |
| `ErrServiceUnavailable` | 503 | 服务不可用 |

## 迁移示例

### 旧代码
```go
if err != nil {
    zap.L().Error("Failed to create payment", zap.Error(err))
    c.JSON(consts.StatusInternalServerError, utils.H{"error": err.Error()})
    return
}
```

### 新代码
```go
if err != nil {
    common.SendError(c, err) // 自动处理错误格式和敏感信息
    return
}
```

### 旧代码
```go
if req.UserID == "" {
    c.JSON(consts.StatusBadRequest, utils.H{"error": "user_id is required"})
    return
}
```

### 新代码
```go
if req.UserID == "" {
    common.SendError(c, common.ErrMissingParameter.WithDetails("user_id is required"))
    return
}
```

## 敏感信息清理

系统会自动清理以下敏感信息：
- password
- secret
- key
- token
- credential
- connection string
- database://
- mysql://
- postgres://

**注意**：在生产环境中，包含敏感信息的错误行会被移除。开发环境中会显示完整错误信息。

## 错误ID追踪

每个错误都会生成唯一的错误ID，用于日志追踪：

```go
// 日志中会包含error_id
zap.L().Error("Error occurred",
    zap.String("error_id", "ERR-1234567890"),
    zap.String("message", "Error message"))
```

## 中间件

### RecoveryHandler
自动捕获panic并返回友好的错误响应。

### ErrorHandler
处理 `c.Errors` 中的错误，统一转换为API错误格式。

## 环境配置

设置开发模式（显示详细错误信息）：

```go
common.IsDevelopment = true // 开发环境
common.IsDevelopment = false // 生产环境（默认）
```

## 最佳实践

1. **使用预定义错误**：优先使用预定义的错误类型
2. **包装内部错误**：使用 `SendError(c, err)` 自动包装
3. **添加详细信息**：仅在开发环境需要时添加 `WithDetails()`
4. **记录日志**：错误会自动记录日志，无需手动记录
5. **避免泄露敏感信息**：系统会自动清理，但也要注意不要在错误消息中直接包含敏感信息

