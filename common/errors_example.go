package common

// 使用示例：
//
// 1. 在handler中使用统一错误处理：
//
//    import "stripe-pay/common"
//
//    func MyHandler(ctx context.Context, c *app.RequestContext) {
//        // 方式1: 使用预定义错误
//        if someCondition {
//            common.SendError(c, common.ErrInvalidRequest)
//            return
//        }
//
//        // 方式2: 创建自定义错误
//        if err != nil {
//            common.SendError(c, common.NewAPIError(400, "Custom error message"))
//            return
//        }
//
//        // 方式3: 包装内部错误（自动清理敏感信息）
//        if err != nil {
//            common.SendError(c, err) // 自动转换为APIError
//            return
//        }
//
//        // 方式4: 带详细信息的错误（仅开发环境显示）
//        if err != nil {
//            common.SendError(c, common.ErrInternalServer.WithDetails("Database connection failed"))
//            return
//        }
//
//        // 成功响应
//        common.SendSuccess(c, data)
//    }
//
// 2. 错误响应格式：
//
//    生产环境：
//    {
//        "code": 400,
//        "message": "Invalid request",
//        "error_id": "ERR-1234567890"
//    }
//
//    开发环境：
//    {
//        "code": 400,
//        "message": "Invalid request",
//        "details": "具体错误信息（包含敏感信息）",
//        "error_id": "ERR-1234567890"
//    }
