package common

import (
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// Response 统一的响应结构
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *APIError   `json:"error,omitempty"`
	Message string      `json:"message,omitempty"`
}

// SendJSON 发送JSON响应
func SendJSON(c *app.RequestContext, code int, data interface{}) {
	c.JSON(code, data)
}

// SendSuccessResponse 发送成功响应
func SendSuccessResponse(c *app.RequestContext, data interface{}) {
	SendJSON(c, consts.StatusOK, Response{
		Success: true,
		Data:    data,
	})
}

// SendErrorResponse 发送错误响应
func SendErrorResponse(c *app.RequestContext, err *APIError) {
	SendJSON(c, err.Code, Response{
		Success: false,
		Error:   err,
	})
}

// SendMessage 发送消息响应
func SendMessage(c *app.RequestContext, message string) {
	SendJSON(c, consts.StatusOK, Response{
		Success: true,
		Message: message,
	})
}

// SendData 发送数据响应（兼容旧代码）
func SendData(c *app.RequestContext, data interface{}) {
	c.JSON(consts.StatusOK, data)
}

// SendH 发送utils.H格式的响应（兼容旧代码）
func SendH(c *app.RequestContext, code int, data utils.H) {
	c.JSON(code, data)
}
