# MySQL 到 PostgreSQL 迁移指南

## 概述

系统已从 MySQL 迁移到 PostgreSQL。本文档说明迁移的主要变更和使用方法。

## 主要变更

### 1. 数据库驱动

- **之前**: `github.com/go-sql-driver/mysql`
- **现在**: `github.com/lib/pq`

### 2. 连接字符串格式

- **之前**: `user:password@tcp(host:port)/database?charset=utf8mb4&parseTime=True&loc=Local`
- **现在**: `postgres://user:password@host:port/database?sslmode=disable`

### 3. SQL 语法差异

#### 占位符
- **之前**: `?` (MySQL)
- **现在**: `$1, $2, $3...` (PostgreSQL)

#### UPSERT 操作
- **之前**: `ON DUPLICATE KEY UPDATE ... VALUES(...)`
- **现在**: `ON CONFLICT (...) DO UPDATE SET ... = EXCLUDED....`

#### 布尔值
- **之前**: `TRUE` / `FALSE` (MySQL)
- **现在**: `true` / `false` (PostgreSQL，小写)

#### 自增主键
- **之前**: `BIGINT UNSIGNED AUTO_INCREMENT`
- **现在**: `BIGSERIAL` 或 `SERIAL`

#### 时间戳更新
- **之前**: `ON UPDATE CURRENT_TIMESTAMP` (MySQL)
- **现在**: 使用触发器函数 `update_updated_at_column()`

#### 信息模式查询
- **之前**: `INFORMATION_SCHEMA.COLUMNS` 和 `INFORMATION_SCHEMA.STATISTICS`
- **现在**: `information_schema.columns` 和 `pg_indexes`

### 4. 配置变更

#### config.yaml

```yaml
database:
  host: "localhost"
  port: 5432  # PostgreSQL 默认端口（之前是 3306）
  user: "postgres"  # 默认用户（之前是 root）
  password: ""
  database: "pay_api"
  # charset 字段已移除（PostgreSQL 使用 UTF-8 编码）
  max_open_conns: 100
  max_idle_conns: 10
  conn_max_lifetime: 3600
```

## 安装 PostgreSQL

### macOS (Homebrew)

```bash
brew install postgresql@14
brew services start postgresql@14
```

### Linux (Ubuntu/Debian)

```bash
sudo apt-get update
sudo apt-get install postgresql postgresql-contrib
sudo systemctl start postgresql
sudo systemctl enable postgresql
```

### 创建数据库和用户

```bash
# 切换到 postgres 用户
sudo -u postgres psql

# 在 PostgreSQL 中执行
CREATE DATABASE pay_api;
CREATE USER your_user WITH PASSWORD 'your_password';
GRANT ALL PRIVILEGES ON DATABASE pay_api TO your_user;
\q
```

## 数据库初始化

### 1. 创建数据库结构

```bash
psql -U postgres -d pay_api -f database/schema.sql
```

或者：

```bash
psql -U postgres -d pay_api < database/schema.sql
```

### 2. 运行迁移（如果需要）

```bash
psql -U postgres -d pay_api -f database/add_idempotency_key.sql
```

## 配置环境变量

```bash
export DB_PASSWORD="your_postgres_password"
```

或在 `config.yaml` 中配置：

```yaml
database:
  host: "localhost"
  port: 5432
  user: "postgres"
  password: "your_password"
  database: "pay_api"
```

## 验证连接

运行应用后，检查日志中是否有：

```
Database connected successfully host=localhost port=5432 database=pay_api
```

## 常见问题

### 1. 连接被拒绝

**错误**: `connection refused`

**解决**:
- 检查 PostgreSQL 服务是否运行: `brew services list` (macOS) 或 `sudo systemctl status postgresql` (Linux)
- 检查端口是否正确（默认 5432）
- 检查 `pg_hba.conf` 配置，确保允许本地连接

### 2. 认证失败

**错误**: `password authentication failed`

**解决**:
- 确认用户名和密码正确
- 检查 PostgreSQL 用户权限
- 如果使用 `peer` 认证，确保系统用户与数据库用户匹配

### 3. 数据库不存在

**错误**: `database "pay_api" does not exist`

**解决**:
```bash
psql -U postgres -c "CREATE DATABASE pay_api;"
```

### 4. 权限不足

**错误**: `permission denied`

**解决**:
```sql
GRANT ALL PRIVILEGES ON DATABASE pay_api TO your_user;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO your_user;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO your_user;
```

## 从 MySQL 迁移数据

如果需要从现有 MySQL 数据库迁移数据：

### 1. 导出 MySQL 数据

```bash
mysqldump -u root -p pay_api > mysql_backup.sql
```

### 2. 转换 SQL 语法

需要手动转换或使用工具：
- 将 `AUTO_INCREMENT` 改为 `SERIAL`
- 将 `ON DUPLICATE KEY UPDATE` 改为 `ON CONFLICT ... DO UPDATE`
- 将 `?` 占位符改为 `$1, $2...`
- 调整数据类型（如 `TEXT` 改为 `TEXT`，`JSON` 改为 `JSONB`）

### 3. 导入到 PostgreSQL

```bash
psql -U postgres -d pay_api < converted_backup.sql
```

## 性能优化建议

1. **连接池**: 已配置连接池参数，可根据实际情况调整
2. **索引**: 所有必要的索引已在 schema.sql 中创建
3. **JSONB**: 使用 `JSONB` 类型存储 metadata，支持高效查询
4. **部分索引**: `uk_idempotency_key` 使用部分索引（WHERE idempotency_key IS NOT NULL），节省空间

## 回滚到 MySQL

如果需要回滚到 MySQL：

1. 恢复 `go.mod` 中的 MySQL 驱动
2. 恢复 `db/db.go` 和 `db/payment.go` 中的 MySQL 语法
3. 恢复 `config.yaml` 中的 MySQL 配置
4. 恢复 SQL 文件

**注意**: 建议使用版本控制（Git）管理代码，可以轻松回滚。

## 测试

运行测试确保一切正常：

```bash
# 编译
go build

# 运行
./stripe-pay

# 测试 API
curl -X GET http://localhost:8080/health
```

## 更新日志

- 2024-01-XX: 从 MySQL 迁移到 PostgreSQL
  - 更新数据库驱动
  - 修改所有 SQL 语句
  - 更新配置文件
  - 更新 SQL 脚本

