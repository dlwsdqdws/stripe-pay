#!/bin/bash
# MySQL 启动脚本

echo "正在尝试启动 MySQL..."

# 方法 1: 使用 Homebrew (macOS)
if command -v brew &> /dev/null; then
    echo "检测到 Homebrew，尝试启动 MySQL..."
    
    # 尝试不同版本的 MySQL
    brew services start mysql@5.7 2>/dev/null && echo "✅ MySQL 5.7 启动成功" && exit 0
    brew services start mysql@8.0 2>/dev/null && echo "✅ MySQL 8.0 启动成功" && exit 0
    brew services start mysql 2>/dev/null && echo "✅ MySQL 启动成功" && exit 0
fi

# 方法 2: 使用 mysql.server (如果存在)
if command -v mysql.server &> /dev/null; then
    echo "使用 mysql.server 启动..."
    mysql.server start && echo "✅ MySQL 启动成功" && exit 0
fi

# 方法 3: 使用 mysqld_safe (如果存在)
if command -v mysqld_safe &> /dev/null; then
    echo "使用 mysqld_safe 启动..."
    mysqld_safe --user=mysql > /dev/null 2>&1 &
    sleep 2
    if mysqladmin ping -h localhost --silent 2>/dev/null; then
        echo "✅ MySQL 启动成功"
        exit 0
    fi
fi

# 方法 4: 使用 systemctl (Linux)
if command -v systemctl &> /dev/null; then
    echo "使用 systemctl 启动..."
    sudo systemctl start mysql 2>/dev/null && echo "✅ MySQL 启动成功" && exit 0
    sudo systemctl start mysqld 2>/dev/null && echo "✅ MySQL 启动成功" && exit 0
fi

echo "❌ 无法自动启动 MySQL"
echo ""
echo "请手动启动 MySQL："
echo "  macOS (Homebrew): brew services start mysql"
echo "  macOS (手动): mysql.server start"
echo "  Linux: sudo systemctl start mysql"
echo ""
echo "或者检查 MySQL 是否已安装："
echo "  brew install mysql"

