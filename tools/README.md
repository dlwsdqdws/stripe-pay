# 工具程序

本目录包含各种工具和辅助程序。

## 工具列表

### verify_redis_connection.go

Redis 连接验证工具。

**功能**:
- 验证 Redis 连接是否正常
- 显示 Redis 配置信息
- 检查缓存功能是否启用

**使用方法**:

```bash
# 运行工具
go run tools/verify_redis_connection.go

# 或编译后运行
go build -o tools/verify_redis tools/verify_redis_connection.go
./tools/verify_redis
```

**输出示例**:

```
正在连接 Redis...
✅ Redis 连接成功！

Redis 配置信息：
  地址: localhost:6379
  数据库: 0
  连接池大小: 10

✅ Redis 缓存功能已启用
```

**用途**:
- 部署前验证 Redis 配置
- 排查 Redis 连接问题
- 检查缓存功能状态

## 注意事项

- 运行前确保 `config.yaml` 已正确配置
- 需要安装 Go 环境
- 需要 Redis 服务运行（如果使用 Redis）

