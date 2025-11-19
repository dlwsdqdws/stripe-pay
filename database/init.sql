-- 初始化数据库脚本 (PostgreSQL)
-- 使用方法: psql -U postgres -d pay_api -f database/init.sql
-- 或者: psql -U postgres -d pay_api < database/init.sql

-- 执行 schema.sql（需要在 psql 中手动执行，或使用 \i 命令）
-- \i database/schema.sql

-- 插入一些测试数据（可选）
-- INSERT INTO user_payment_info (user_id, has_paid) 
-- VALUES ('test_user_001', false) 
-- ON CONFLICT (user_id) DO UPDATE SET user_id = user_payment_info.user_id;
