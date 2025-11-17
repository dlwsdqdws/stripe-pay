#!/bin/bash
# 数据库初始化脚本

echo "正在创建数据库和表..."

# 提示输入 MySQL root 密码
read -sp "请输入 MySQL root 密码: " MYSQL_PASSWORD
echo ""

# 执行 SQL 脚本
mysql -u root -p"$MYSQL_PASSWORD" < database/schema.sql

if [ $? -eq 0 ]; then
    echo "✅ 数据库和表创建成功！"
    echo ""
    echo "数据库: pay_api"
    echo "表:"
    echo "  - payment_history (支付历史表)"
    echo "  - user_payment_info (用户支付信息表)"
else
    echo "❌ 数据库创建失败，请检查 MySQL 服务是否运行"
    echo "启动 MySQL: brew services start mysql (macOS)"
    exit 1
fi

