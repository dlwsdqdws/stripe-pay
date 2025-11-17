package biz

import (
	"strings"
	"testing"
)

// TestValidateUserID 测试用户ID验证
func TestValidateUserID(t *testing.T) {
	tests := []struct {
		name    string
		userID  string
		wantErr bool
	}{
		{"有效用户ID-字母数字", "user123", false},
		{"有效用户ID-下划线", "user_123", false},
		{"有效用户ID-点号", "user.123", false},
		{"有效用户ID-连字符", "user-123", false},
		{"有效用户ID-中文", "用户123", false},
		{"有效用户ID-混合", "user_123.测试", false},
		{"空用户ID", "", true},
		{"超长用户ID", strings.Repeat("a", MaxUserIDLength+1), true},
		{"包含特殊字符", "user@123", true},
		{"包含空格", "user 123", true},
		{"单字符", "a", false},
		{"最大长度", strings.Repeat("a", MaxUserIDLength), false},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUserID(tt.userID)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateUserID(%q) error = %v, wantErr %v", tt.userID, err, tt.wantErr)
			}
		})
	}
}

// TestValidateDescription 测试描述验证
func TestValidateDescription(t *testing.T) {
	tests := []struct {
		name        string
		description string
		wantErr     bool
	}{
		{"有效描述", "这是一个测试描述", false},
		{"空描述", "", false}, // 空描述是允许的
		{"最大长度描述", strings.Repeat("a", MaxDescriptionLength), false},
		{"超长描述", strings.Repeat("a", MaxDescriptionLength+1), true},
		{"正常描述", "Payment for premium features", false},
		{"包含特殊字符", "描述@#$%", false}, // 描述允许特殊字符
		{"多行描述", "第一行\n第二行", false},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDescription(tt.description)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDescription(%q) error = %v, wantErr %v", tt.description, err, tt.wantErr)
			}
		})
	}
}

// TestValidateAmount 测试金额验证
func TestValidateAmount(t *testing.T) {
	tests := []struct {
		name    string
		amount  int64
		wantErr bool
	}{
		{"最小金额", MinAmount, false},
		{"正常金额", 5900, false},
		{"最大金额", MaxAmount, false},
		{"零金额", 0, true},
		{"负数金额", -100, true},
		{"超过最大金额", MaxAmount + 1, true},
		{"边界值-1", MinAmount - 1, true},
		{"边界值+1", MinAmount + 1, false},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAmount(tt.amount)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAmount(%d) error = %v, wantErr %v", tt.amount, err, tt.wantErr)
			}
		})
	}
}

// TestValidateCurrency 测试币种验证
func TestValidateCurrency(t *testing.T) {
	tests := []struct {
		name     string
		currency string
		wantErr  bool
	}{
		{"有效币种-HKD", "hkd", false},
		{"有效币种-USD", "usd", false},
		{"有效币种-CNY", "cny", false},
		{"有效币种-EUR", "eur", false},
		{"有效币种-GBP", "gbp", false},
		{"有效币种-JPY", "jpy", false},
		{"无效币种", "invalid", true},
		{"空币种", "", false}, // 空币种会被转换为小写，验证函数会处理
		{"大写币种", "HKD", false}, // 验证函数会转换为小写
		{"混合大小写", "Hkd", false}, // 验证函数会转换为小写
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCurrency(tt.currency)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCurrency(%q) error = %v, wantErr %v", tt.currency, err, tt.wantErr)
			}
		})
	}
}

// TestValidateURL 测试URL验证
func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"有效URL-HTTP", "http://example.com", false},
		{"有效URL-HTTPS", "https://example.com", false},
		{"有效URL-带路径", "https://example.com/path", false},
		{"有效URL-带查询参数", "https://example.com?param=value", false},
		{"空URL", "", false}, // 空URL是允许的（可选字段）
		{"无效URL", "not-a-url", true},
		{"无效协议", "ftp://example.com", true},
		{"超长URL", "https://example.com/" + strings.Repeat("a", MaxURLLength), true},
		{"有效URL-端口", "https://example.com:8080", false},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

// TestValidateClient 测试客户端类型验证
func TestValidateClient(t *testing.T) {
	tests := []struct {
		name    string
		client  string
		wantErr bool
	}{
		{"有效客户端-web", "web", false},
		{"有效客户端-mobile", "mobile", false},
		{"空客户端", "", false}, // 空客户端是允许的（会使用默认值）
		{"无效客户端", "desktop", true},
		{"大写客户端", "WEB", false}, // 验证函数会转换为小写
		{"混合大小写", "Web", false}, // 验证函数会转换为小写
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateClient(tt.client)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateClient(%q) error = %v, wantErr %v", tt.client, err, tt.wantErr)
			}
		})
	}
}

// TestValidatePaymentIntentID 测试PaymentIntent ID验证
func TestValidatePaymentIntentID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"有效ID", "pi_1234567890abcdefghijklmn", false}, // 需要24个字符
		{"有效ID-标准格式", "pi_3SSrQY6xpYAaGcYp0CHAb3nG", false},
		{"空ID", "", true},
		{"无效格式", "invalid_id", true},
		{"太短", "pi_123", true},
		{"不包含前缀", "1234567890abcdef", true},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePaymentIntentID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePaymentIntentID(%q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
			}
		})
	}
}

// TestValidateRefundReason 测试退款原因验证
func TestValidateRefundReason(t *testing.T) {
	tests := []struct {
		name    string
		reason  string
		wantErr bool
	}{
		{"有效原因-duplicate", "duplicate", false},
		{"有效原因-fraudulent", "fraudulent", false},
		{"有效原因-requested_by_customer", "requested_by_customer", false},
		{"空原因", "", false}, // 空原因是允许的（可选字段）
		{"无效原因", "invalid_reason", true},
		{"大写原因", "DUPLICATE", false}, // 验证函数会转换为小写
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRefundReason(tt.reason)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRefundReason(%q) error = %v, wantErr %v", tt.reason, err, tt.wantErr)
			}
		})
	}
}

// TestValidatePaymentStatus 测试支付状态验证
func TestValidatePaymentStatus(t *testing.T) {
	tests := []struct {
		name    string
		status  string
		wantErr bool
	}{
		{"有效状态-succeeded", "succeeded", false},
		{"有效状态-failed", "failed", false},
		{"有效状态-canceled", "canceled", false},
		{"有效状态-pending", "pending", false},
		{"有效状态-processing", "processing", false},
		{"无效状态", "invalid_status", true},
		{"空状态", "", true},
		{"大写状态", "SUCCEEDED", false}, // 验证函数会转换为小写
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePaymentStatus(tt.status)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePaymentStatus(%q) error = %v, wantErr %v", tt.status, err, tt.wantErr)
			}
		})
	}
}

// TestValidateReceiptData 测试收据数据验证
func TestValidateReceiptData(t *testing.T) {
	tests := []struct {
		name        string
		receiptData string
		wantErr     bool
	}{
		{"有效收据数据", "base64encodeddata123", false},
		{"空收据数据", "", true},
		{"超长收据数据", strings.Repeat("a", 100001), true}, // 最大长度为100000
		{"有效收据数据-边界", strings.Repeat("a", 100000), false},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateReceiptData(tt.receiptData)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateReceiptData(%q) error = %v, wantErr %v", tt.receiptData, err, tt.wantErr)
			}
		})
	}
}

// BenchmarkValidateUserID 性能测试：用户ID验证
func BenchmarkValidateUserID(b *testing.B) {
	userID := "test_user_123"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateUserID(userID)
	}
}

// BenchmarkValidateAmount 性能测试：金额验证
func BenchmarkValidateAmount(b *testing.B) {
	amount := int64(5900)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateAmount(amount)
	}
}

