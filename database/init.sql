-- 初始化数据库脚本
-- 使用方法: mysql -u root -p < database/init.sql

SOURCE database/schema.sql;

-- 插入一些测试数据（可选）
-- INSERT INTO user_payment_info (user_id, has_paid) VALUES ('test_user_001', FALSE) ON DUPLICATE KEY UPDATE user_id=user_id;

