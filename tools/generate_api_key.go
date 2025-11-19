package main

import (
	"fmt"
	"stripe-pay/common"
)

func main() {
	// 生成普通 API Key
	apiKey, err := common.GenerateAPIKey()
	if err != nil {
		fmt.Printf("❌ 生成 API Key 失败: %v\n", err)
		return
	}

	// 生成管理员 API Key
	adminKey, err := common.GenerateAPIKey()
	if err != nil {
		fmt.Printf("❌ 生成管理员 API Key 失败: %v\n", err)
		return
	}

	fmt.Println("✅ API Key 生成成功！")
	fmt.Println("")
	fmt.Println("普通 API Key（用于普通接口）：")
	fmt.Printf("  %s\n", apiKey)
	fmt.Println("")
	fmt.Println("管理员 API Key（用于管理员接口）：")
	fmt.Printf("  %s\n", adminKey)
	fmt.Println("")
	fmt.Println("使用方法：")
	fmt.Println("  1. 设置环境变量：")
	fmt.Printf("     export API_KEYS=\"%s\"\n", apiKey)
	fmt.Printf("     export ADMIN_API_KEYS=\"%s\"\n", adminKey)
	fmt.Println("")
	fmt.Println("  2. 或者在 config.yaml 中配置（未来支持）")
	fmt.Println("")
	fmt.Println("  3. 在请求头中添加：")
	fmt.Println("     X-API-Key: <your-api-key>")
	fmt.Println("     或")
	fmt.Println("     Authorization: Bearer <your-api-key>")
	fmt.Println("")
	fmt.Println("⚠️  请妥善保管这些密钥，不要泄露！")
}
