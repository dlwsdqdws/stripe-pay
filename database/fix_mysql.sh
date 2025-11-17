#!/bin/bash
# MySQL 修复和初始化脚本

echo "=========================================="
echo "MySQL 修复和初始化"
echo "=========================================="
echo ""

MYSQL_DATA_DIR="/opt/anaconda3/data"
MYSQL_BIN_DIR="/opt/anaconda3/bin"

# 检查 MySQL 是否已安装
if [ ! -f "$MYSQL_BIN_DIR/mysqld" ]; then
    echo "❌ MySQL 未正确安装"
    echo ""
    echo "建议使用 Homebrew 安装 MySQL："
    echo "  brew install mysql"
    echo "  brew services start mysql"
    exit 1
fi

# 创建数据目录
if [ ! -d "$MYSQL_DATA_DIR" ]; then
    echo "正在创建 MySQL 数据目录..."
    mkdir -p "$MYSQL_DATA_DIR"
    chmod 755 "$MYSQL_DATA_DIR"
fi

# 初始化 MySQL（如果数据目录为空）
if [ -z "$(ls -A $MYSQL_DATA_DIR 2>/dev/null)" ]; then
    echo "正在初始化 MySQL 数据目录..."
    $MYSQL_BIN_DIR/mysqld --initialize-insecure --datadir=$MYSQL_DATA_DIR --user=$(whoami)
    
    if [ $? -eq 0 ]; then
        echo "✅ MySQL 初始化成功"
    else
        echo "❌ MySQL 初始化失败"
        echo "可能需要 root 权限，请尝试："
        echo "  sudo $MYSQL_BIN_DIR/mysqld --initialize-insecure --datadir=$MYSQL_DATA_DIR"
        exit 1
    fi
fi

# 尝试启动 MySQL
echo ""
echo "正在启动 MySQL..."
$MYSQL_BIN_DIR/mysql.server start

sleep 2

# 检查是否启动成功
if mysqladmin ping -h localhost --silent 2>/dev/null; then
    echo "✅ MySQL 启动成功！"
    echo ""
    echo "下一步："
    echo "  1. 运行 ./database/quick_setup.sh 创建数据库"
    echo "  2. 或手动执行: mysql -u root < database/schema.sql"
else
    echo "❌ MySQL 启动失败"
    echo ""
    echo "请检查错误日志："
    echo "  tail -f $MYSQL_DATA_DIR/*.err"
    echo ""
    echo "或者考虑使用 Homebrew 重新安装 MySQL："
    echo "  brew install mysql"
    echo "  brew services start mysql"
fi

