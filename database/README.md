# 数据库设置指南

## 安装 MySQL

### macOS
```bash
# 使用 Homebrew 安装
brew install mysql

# 启动 MySQL 服务
brew services start mysql

# 或者手动启动
mysql.server start
```

### Linux (Ubuntu/Debian)
```bash
sudo apt-get update
sudo apt-get install mysql-server
sudo systemctl start mysql
sudo systemctl enable mysql
```

### 设置 root 密码
```bash
mysql_secure_installation
```

## 创建数据库和表

### 方法 1: 使用 SQL 脚本
```bash
# 登录 MySQL
mysql -u root -p

# 在 MySQL 中执行
source /path/to/pay-api/database/schema.sql
```

### 方法 2: 直接执行
```bash
mysql -u root -p < database/schema.sql
```

## 数据库配置

在 `config.yaml` 中添加数据库配置：
```yaml
database:
  host: localhost
  port: 3306
  user: root
  password: your_password
  database: pay_api
  charset: utf8mb4
  max_open_conns: 100
  max_idle_conns: 10
  conn_max_lifetime: 3600
```

## 表结构说明

### payment_history（支付历史表）
- 存储所有支付记录
- 包含支付状态、金额、支付方式等信息
- 支持按用户ID、状态、时间查询

### user_payment_info（用户支付信息表）
- 存储用户支付状态摘要
- 记录是否支付成功过、首次/最近支付时间
- 统计总支付次数和累计金额

## 测试连接

```bash
mysql -u root -p -e "USE pay_api; SHOW TABLES;"
```

