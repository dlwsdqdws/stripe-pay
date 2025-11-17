#!/bin/bash
# 快速数据库设置脚本

echo "=========================================="
echo "Pay API 数据库初始化"
echo "=========================================="
echo ""

# 检查 MySQL 是否运行
echo "正在检查 MySQL 服务状态..."
if ! mysqladmin ping -h localhost --silent 2>/dev/null; then
    echo "⚠️  MySQL 服务未运行"
    echo ""
    
    # 尝试启动 MySQL
    if command -v brew &> /dev/null; then
        echo "正在尝试通过 Homebrew 启动 MySQL..."
        brew services start mysql@5.7 2>/dev/null || brew services start mysql 2>/dev/null
        sleep 3
        
        # 再次检查
        if ! mysqladmin ping -h localhost --silent 2>/dev/null; then
            echo ""
            echo "❌ 自动启动失败，请手动启动 MySQL："
            echo ""
            echo "方法 1 (Homebrew):"
            echo "   brew services start mysql"
            echo ""
            echo "方法 2 (手动启动):"
            echo "   mysql.server start"
            echo ""
            echo "方法 3 (如果使用 MySQL 8.0+):"
            echo "   mysqld_safe --user=mysql &"
            echo ""
            echo "启动后，请重新运行此脚本"
            exit 1
        fi
    else
        echo "❌ 无法自动启动 MySQL，请手动启动："
        echo ""
        echo "macOS:"
        echo "   brew services start mysql"
        echo "   或: mysql.server start"
        echo ""
        echo "Linux:"
        echo "   sudo systemctl start mysql"
        echo "   或: sudo service mysql start"
        echo ""
        echo "启动后，请重新运行此脚本"
        exit 1
    fi
fi

echo "✅ MySQL 服务运行中"
echo ""

# 提示输入 MySQL root 密码
read -sp "请输入 MySQL root 密码（直接回车如果无密码）: " MYSQL_PASSWORD
echo ""

# 如果没有密码，使用空密码
if [ -z "$MYSQL_PASSWORD" ]; then
    MYSQL_CMD="mysql -u root"
else
    MYSQL_CMD="mysql -u root -p$MYSQL_PASSWORD"
fi

echo "正在创建数据库和表..."

# 执行 SQL 脚本
$MYSQL_CMD < database/schema.sql 2>&1

if [ $? -eq 0 ]; then
    echo ""
    echo "=========================================="
    echo "✅ 数据库和表创建成功！"
    echo "=========================================="
    echo ""
    echo "数据库: pay_api"
    echo "表:"
    echo "  📋 payment_history - 支付历史记录表"
    echo "  👤 user_payment_info - 用户支付信息表"
    echo "  ⚙️  payment_config - 支付金额配置表"
    echo ""
    echo "下一步："
    echo "  1. 在 config.yaml 中配置数据库连接信息"
    echo "  2. 重启后端服务"
    echo ""
else
    echo ""
    echo "❌ 数据库创建失败"
    echo "请检查："
    echo "  1. MySQL 服务是否运行"
    echo "  2. root 密码是否正确"
    echo "  3. 是否有创建数据库的权限"
    exit 1
fi

