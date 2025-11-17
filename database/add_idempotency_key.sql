-- 添加幂等性密钥字段到payment_history表
-- 用于防止重复创建支付订单
-- 
-- 使用方法: 
--   mysql -u root -p pay_api < database/add_idempotency_key.sql
--   或者查看 config.yaml 中的 database.user 配置，使用对应的用户名
-- 
-- 注意：此脚本会检查字段是否已存在，可以安全地重复执行

USE pay_api;

-- 检查字段是否存在，如果不存在则添加
SET @dbname = DATABASE();
SET @tablename = 'payment_history';
SET @columnname = 'idempotency_key';
SET @preparedStatement = (SELECT IF(
  (
    SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
    WHERE
      (table_name = @tablename)
      AND (table_schema = @dbname)
      AND (column_name = @columnname)
  ) > 0,
  'SELECT 1', -- 字段已存在，不执行任何操作
  CONCAT('ALTER TABLE ', @tablename, ' ADD COLUMN ', @columnname, ' VARCHAR(255) NULL COMMENT ''幂等性密钥，用于防止重复请求'' AFTER payment_id')
));
PREPARE alterIfNotExists FROM @preparedStatement;
EXECUTE alterIfNotExists;
DEALLOCATE PREPARE alterIfNotExists;

-- 检查索引是否存在，如果不存在则添加
SET @indexname = 'uk_idempotency_key';
SET @preparedStatement = (SELECT IF(
  (
    SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS
    WHERE
      (table_schema = @dbname)
      AND (table_name = @tablename)
      AND (index_name = @indexname)
  ) > 0,
  'SELECT 1', -- 索引已存在，不执行任何操作
  CONCAT('ALTER TABLE ', @tablename, ' ADD UNIQUE INDEX ', @indexname, ' (idempotency_key)')
));
PREPARE alterIfNotExists FROM @preparedStatement;
EXECUTE alterIfNotExists;
DEALLOCATE PREPARE alterIfNotExists;

-- 验证迁移结果
SELECT 
    CASE 
        WHEN COUNT(*) > 0 THEN '✅ idempotency_key 字段已存在'
        ELSE '❌ idempotency_key 字段不存在'
    END as column_check
FROM INFORMATION_SCHEMA.COLUMNS
WHERE table_schema = @dbname
  AND table_name = @tablename
  AND column_name = @columnname;

SELECT 
    CASE 
        WHEN COUNT(*) > 0 THEN '✅ uk_idempotency_key 索引已存在'
        ELSE '❌ uk_idempotency_key 索引不存在'
    END as index_check
FROM INFORMATION_SCHEMA.STATISTICS
WHERE table_schema = @dbname
  AND table_name = @tablename
  AND index_name = @indexname;

