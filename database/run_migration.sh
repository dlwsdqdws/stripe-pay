#!/bin/bash

# 数据库迁移脚本 - 自动从配置文件读取数据库信息

echo "=========================================="
echo "幂等性功能数据库迁移"
echo "=========================================="
echo ""

# 检查配置文件是否存在
if [ ! -f "config.yaml" ]; then
    echo "❌ 配置文件 config.yaml 不存在"
    echo "请先创建配置文件，或手动执行迁移："
    echo "  mysql -u root -p pay_api < database/add_idempotency_key.sql"
    exit 1
fi

# 从配置文件读取数据库信息（简单解析，假设格式标准）
DB_USER=$(grep -A 10 "^database:" config.yaml | grep "^  user:" | awk '{print $2}' | tr -d '"' | tr -d "'")
DB_NAME=$(grep -A 10 "^database:" config.yaml | grep "^  database:" | awk '{print $2}' | tr -d '"' | tr -d "'")

# 如果解析失败，使用默认值
if [ -z "$DB_USER" ]; then
    DB_USER="root"
    echo "⚠️  无法从配置文件读取用户名，使用默认值: root"
fi

if [ -z "$DB_NAME" ]; then
    DB_NAME="pay_api"
    echo "⚠️  无法从配置文件读取数据库名，使用默认值: pay_api"
fi

echo "数据库配置："
echo "  用户名: $DB_USER"
echo "  数据库: $DB_NAME"
echo ""

# 检查迁移脚本是否存在
if [ ! -f "database/add_idempotency_key.sql" ]; then
    echo "❌ 迁移脚本不存在: database/add_idempotency_key.sql"
    exit 1
fi

echo "正在执行迁移..."
echo ""

# 执行迁移
mysql -u "$DB_USER" -p "$DB_NAME" < database/add_idempotency_key.sql

if [ $? -eq 0 ]; then
    echo ""
    echo "=========================================="
    echo "✅ 迁移完成！"
    echo "=========================================="
    echo ""
    echo "下一步："
    echo "  1. 重启服务以应用更改"
    echo "  2. 服务启动时会自动验证数据库结构"
else
    echo ""
    echo "=========================================="
    echo "❌ 迁移失败"
    echo "=========================================="
    echo ""
    echo "可能的原因："
    echo "  1. 数据库用户名或密码错误"
    echo "  2. 数据库不存在"
    echo "  3. 没有执行权限"
    echo ""
    echo "请手动执行："
    echo "  mysql -u $DB_USER -p $DB_NAME < database/add_idempotency_key.sql"
    exit 1
fi

