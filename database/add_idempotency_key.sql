-- 添加幂等性密钥字段到payment_history表 (PostgreSQL)
-- 用于防止重复创建支付订单
-- 
-- 使用方法: 
--   psql -U postgres -d pay_api -f database/add_idempotency_key.sql
--   或者查看 config.yaml 中的 database.user 配置，使用对应的用户名
-- 
-- 注意：此脚本会检查字段是否已存在，可以安全地重复执行

-- 检查字段是否存在，如果不存在则添加
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'payment_history'
          AND column_name = 'idempotency_key'
    ) THEN
        ALTER TABLE payment_history 
        ADD COLUMN idempotency_key VARCHAR(255) NULL;
        
        COMMENT ON COLUMN payment_history.idempotency_key IS '幂等性密钥，用于防止重复请求';
        
        RAISE NOTICE '✅ idempotency_key 字段已添加';
    ELSE
        RAISE NOTICE 'ℹ️  idempotency_key 字段已存在，跳过添加';
    END IF;
END $$;

-- 检查索引是否存在，如果不存在则添加
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_indexes
        WHERE schemaname = current_schema()
          AND tablename = 'payment_history'
          AND indexname = 'uk_idempotency_key'
    ) THEN
        CREATE UNIQUE INDEX uk_idempotency_key 
        ON payment_history(idempotency_key) 
        WHERE idempotency_key IS NOT NULL;
        
        RAISE NOTICE '✅ uk_idempotency_key 索引已创建';
    ELSE
        RAISE NOTICE 'ℹ️  uk_idempotency_key 索引已存在，跳过创建';
    END IF;
END $$;

-- 验证迁移结果
SELECT 
    CASE 
        WHEN COUNT(*) > 0 THEN '✅ idempotency_key 字段已存在'
        ELSE '❌ idempotency_key 字段不存在'
    END as column_check
FROM information_schema.columns
WHERE table_schema = current_schema()
  AND table_name = 'payment_history'
  AND column_name = 'idempotency_key';

SELECT 
    CASE 
        WHEN COUNT(*) > 0 THEN '✅ uk_idempotency_key 索引已存在'
        ELSE '❌ uk_idempotency_key 索引不存在'
    END as index_check
FROM pg_indexes
WHERE schemaname = current_schema()
  AND tablename = 'payment_history'
  AND indexname = 'uk_idempotency_key';
