# MySQL 启动指南

## 问题：无法连接到 MySQL

如果遇到 `ERROR 2002 (HY000): Can't connect to local MySQL server` 错误，说明 MySQL 服务未运行。

## 解决方案

### 方法 1: 使用 mysql.server（推荐，适用于 Anaconda 安装的 MySQL）

```bash
# 启动 MySQL
/opt/anaconda3/bin/mysql.server start

# 停止 MySQL
/opt/anaconda3/bin/mysql.server stop

# 重启 MySQL
/opt/anaconda3/bin/mysql.server restart
```

### 方法 2: 使用 Homebrew（如果通过 Homebrew 安装）

```bash
# 启动 MySQL
brew services start mysql

# 停止 MySQL
brew services stop mysql

# 查看状态
brew services list | grep mysql
```

### 方法 3: 手动启动（如果以上方法都不行）

```bash
# 查找 MySQL 安装位置
which mysql
which mysqld

# 通常 MySQL 数据目录在：
# macOS: /usr/local/var/mysql
# Anaconda: /opt/anaconda3/var/mysql

# 手动启动（需要找到 mysqld 路径）
mysqld_safe --datadir=/opt/anaconda3/var/mysql &
```

## 验证 MySQL 是否运行

```bash
# 方法 1: 使用 mysqladmin
mysqladmin ping -h localhost

# 方法 2: 检查进程
ps aux | grep mysql

# 方法 3: 尝试连接
mysql -u root -p -e "SELECT 1;"
```

## 创建数据库和表

启动 MySQL 后，运行：

```bash
./database/quick_setup.sh
```

或者手动执行：

```bash
mysql -u root -p < database/schema.sql
```

## 常见问题

### 1. 找不到 mysql.server

如果 `mysql.server` 不在 PATH 中，使用完整路径：
```bash
/opt/anaconda3/bin/mysql.server start
```

### 2. 权限问题

如果启动失败，可能需要：
```bash
sudo /opt/anaconda3/bin/mysql.server start
```

### 3. Socket 文件位置不对

如果提示找不到 socket 文件，可以指定：
```bash
mysql -u root -p --socket=/tmp/mysql.sock
# 或
mysql -u root -p --socket=/opt/anaconda3/var/mysql/mysql.sock
```

### 4. 端口被占用

检查端口 3306 是否被占用：
```bash
lsof -i :3306
```

## 下一步

1. ✅ 启动 MySQL 服务
2. ✅ 运行 `./database/quick_setup.sh` 创建数据库
3. ✅ 在 `config.yaml` 中配置数据库连接信息
4. ✅ 重启后端服务

