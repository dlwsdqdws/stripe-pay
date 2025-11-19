package handlers

import (
	"bytes"
	"encoding/json"
	"stripe-pay/biz/models"
	"testing"
)

// 注意：由于handlers包在init时创建了paymentService，这需要配置初始化
// 在单元测试环境中，这些测试会被跳过
// 在实际集成测试中，应该先初始化配置

// TestGetIdempotencyKey 测试获取幂等性密钥
// 注意：这个测试需要完整的Hertz环境，暂时跳过
// 在实际环境中，可以通过集成测试来验证getIdempotencyKey的功能
func TestGetIdempotencyKey(t *testing.T) {
	t.Skip("Skipping - requires full Hertz server setup. Test getIdempotencyKey through integration tests.")
	
	// 测试逻辑说明：
	// getIdempotencyKey函数应该：
	// 1. 优先从"Idempotency-Key" header获取
	// 2. 如果没有，从"X-Idempotency-Key" header获取
	// 3. 如果都没有，返回空字符串
}

// TestCreateStripePayment_InvalidRequest 测试无效请求
func TestCreateStripePayment_InvalidRequest(t *testing.T) {
	// 这个测试需要完整的Hertz环境，这里只做基本结构测试
	// 实际测试需要mock服务层
	t.Skip("Skipping - requires full Hertz server setup and service mocking")
}

// TestGetPricing 测试获取定价信息
func TestGetPricing(t *testing.T) {
	// 这个测试需要完整的Hertz环境
	// 实际测试需要mock服务层
	t.Skip("Skipping - requires full Hertz server setup and service mocking")
}

// TestPaymentResponse 测试PaymentResponse结构
func TestPaymentResponse(t *testing.T) {
	response := models.PaymentResponse{
		ClientSecret:    "pi_test_secret",
		PaymentID:       "payment_123",
		PaymentIntentID: "pi_123456",
	}
	
	// 测试JSON序列化
	jsonData, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal PaymentResponse: %v", err)
	}
	
	// 测试JSON反序列化
	var decoded models.PaymentResponse
	err = json.Unmarshal(jsonData, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal PaymentResponse: %v", err)
	}
	
	if decoded.ClientSecret != response.ClientSecret {
		t.Errorf("ClientSecret mismatch: got %q, want %q", decoded.ClientSecret, response.ClientSecret)
	}
	
	if decoded.PaymentID != response.PaymentID {
		t.Errorf("PaymentID mismatch: got %q, want %q", decoded.PaymentID, response.PaymentID)
	}
	
	if decoded.PaymentIntentID != response.PaymentIntentID {
		t.Errorf("PaymentIntentID mismatch: got %q, want %q", decoded.PaymentIntentID, response.PaymentIntentID)
	}
}

// TestCreatePaymentRequest 测试CreatePaymentRequest结构
func TestCreatePaymentRequest(t *testing.T) {
	req := models.CreatePaymentRequest{
		UserID:      "test_user_123",
		Description: "Test payment",
	}
	
	// 测试JSON序列化
	jsonData, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal CreatePaymentRequest: %v", err)
	}
	
	// 测试JSON反序列化
	var decoded models.CreatePaymentRequest
	err = json.Unmarshal(jsonData, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal CreatePaymentRequest: %v", err)
	}
	
	if decoded.UserID != req.UserID {
		t.Errorf("UserID mismatch: got %q, want %q", decoded.UserID, req.UserID)
	}
	
	if decoded.Description != req.Description {
		t.Errorf("Description mismatch: got %q, want %q", decoded.Description, req.Description)
	}
}

// TestPricingResponse 测试PricingResponse结构
func TestPricingResponse(t *testing.T) {
	response := models.PricingResponse{
		Amount:   5900,
		Currency: "hkd",
		Label:    "HK$59",
	}
	
	// 测试JSON序列化
	jsonData, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Failed to marshal PricingResponse: %v", err)
	}
	
	// 验证JSON内容
	if !bytes.Contains(jsonData, []byte("5900")) {
		t.Error("JSON should contain amount 5900")
	}
	
	if !bytes.Contains(jsonData, []byte("hkd")) {
		t.Error("JSON should contain currency 'hkd'")
	}
}

// TestAllPaymentModels 测试所有支付模型
func TestAllPaymentModels(t *testing.T) {
	// 测试CreateWeChatPaymentRequest
	wechatReq := models.CreateWeChatPaymentRequest{
		UserID:      "user123",
		Description: "WeChat payment",
		ReturnURL:   "https://example.com/return",
		Client:      "web",
	}
	
	jsonData, err := json.Marshal(wechatReq)
	if err != nil {
		t.Fatalf("Failed to marshal CreateWeChatPaymentRequest: %v", err)
	}
	
	var decoded models.CreateWeChatPaymentRequest
	err = json.Unmarshal(jsonData, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal CreateWeChatPaymentRequest: %v", err)
	}
	
	if decoded.Client != "web" {
		t.Errorf("Client mismatch: got %q, want 'web'", decoded.Client)
	}
	
	
	// 测试RefundRequest
	refundReq := models.RefundRequest{
		PaymentIntentID: "pi_123456",
		Amount:          1000,
		Reason:          "duplicate",
	}
	
	jsonData, err = json.Marshal(refundReq)
	if err != nil {
		t.Fatalf("Failed to marshal RefundRequest: %v", err)
	}
	
	var decodedRefund models.RefundRequest
	err = json.Unmarshal(jsonData, &decodedRefund)
	if err != nil {
		t.Fatalf("Failed to unmarshal RefundRequest: %v", err)
	}
	
	if decodedRefund.PaymentIntentID != "pi_123456" {
		t.Errorf("PaymentIntentID mismatch: got %q, want 'pi_123456'", decodedRefund.PaymentIntentID)
	}
}

