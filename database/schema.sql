-- Pay API 数据库结构 (PostgreSQL)
-- 创建数据库
-- 注意：PostgreSQL 中需要先创建数据库，不能在这里创建
-- CREATE DATABASE pay_api;
-- \c pay_api;

-- 1. 支付历史表（存储所有支付记录）
CREATE TABLE IF NOT EXISTS payment_history (
    id BIGSERIAL PRIMARY KEY,
    payment_intent_id VARCHAR(255) NOT NULL UNIQUE,
    payment_id VARCHAR(255) NOT NULL,
    idempotency_key VARCHAR(255) NULL,
    user_id VARCHAR(255) NOT NULL,
    amount BIGINT NOT NULL,
    currency VARCHAR(10) NOT NULL DEFAULT 'hkd',
    status VARCHAR(50) NOT NULL,
    payment_method VARCHAR(50) NOT NULL,
    description TEXT,
    metadata JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_amount_positive CHECK (amount > 0)
);

-- 创建索引
CREATE INDEX IF NOT EXISTS idx_payment_history_user_id ON payment_history(user_id);
CREATE INDEX IF NOT EXISTS idx_payment_history_payment_intent_id ON payment_history(payment_intent_id);
CREATE INDEX IF NOT EXISTS idx_payment_history_status ON payment_history(status);
CREATE INDEX IF NOT EXISTS idx_payment_history_created_at ON payment_history(created_at);
CREATE UNIQUE INDEX IF NOT EXISTS uk_idempotency_key ON payment_history(idempotency_key) WHERE idempotency_key IS NOT NULL;

-- 创建更新时间触发器函数
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- 为 payment_history 表创建更新时间触发器
DROP TRIGGER IF EXISTS update_payment_history_updated_at ON payment_history;
CREATE TRIGGER update_payment_history_updated_at
    BEFORE UPDATE ON payment_history
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- 添加表注释
COMMENT ON TABLE payment_history IS '支付历史记录表';
COMMENT ON COLUMN payment_history.payment_intent_id IS 'Stripe PaymentIntent ID';
COMMENT ON COLUMN payment_history.payment_id IS '内部支付ID (UUID)';
COMMENT ON COLUMN payment_history.idempotency_key IS '幂等性密钥，用于防止重复请求';
COMMENT ON COLUMN payment_history.user_id IS '用户ID';
COMMENT ON COLUMN payment_history.amount IS '支付金额（分）';
COMMENT ON COLUMN payment_history.currency IS '币种';
COMMENT ON COLUMN payment_history.status IS '支付状态: succeeded, pending, failed, canceled等';
COMMENT ON COLUMN payment_history.payment_method IS '支付方式: card, wechat_pay, alipay, apple_pay等';
COMMENT ON COLUMN payment_history.description IS '支付描述';
COMMENT ON COLUMN payment_history.metadata IS '额外元数据（JSON格式）';

-- 2. 用户支付信息表（存储用户是否支付成功过）
CREATE TABLE IF NOT EXISTS user_payment_info (
    id BIGSERIAL PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL UNIQUE,
    has_paid BOOLEAN NOT NULL DEFAULT false,
    first_payment_at TIMESTAMP NULL,
    last_payment_at TIMESTAMP NULL,
    total_payment_count INT NOT NULL DEFAULT 0,
    total_payment_amount BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 创建索引
CREATE INDEX IF NOT EXISTS idx_user_payment_info_user_id ON user_payment_info(user_id);
CREATE INDEX IF NOT EXISTS idx_user_payment_info_has_paid ON user_payment_info(has_paid);

-- 为 user_payment_info 表创建更新时间触发器
DROP TRIGGER IF EXISTS update_user_payment_info_updated_at ON user_payment_info;
CREATE TRIGGER update_user_payment_info_updated_at
    BEFORE UPDATE ON user_payment_info
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- 添加表注释
COMMENT ON TABLE user_payment_info IS '用户支付信息表';
COMMENT ON COLUMN user_payment_info.user_id IS '用户ID';
COMMENT ON COLUMN user_payment_info.has_paid IS '是否支付成功过';
COMMENT ON COLUMN user_payment_info.first_payment_at IS '首次支付成功时间';
COMMENT ON COLUMN user_payment_info.last_payment_at IS '最近一次支付成功时间';
COMMENT ON COLUMN user_payment_info.total_payment_count IS '总支付成功次数';
COMMENT ON COLUMN user_payment_info.total_payment_amount IS '累计支付金额（分）';

-- 3. 支付金额配置表（存储需要支付的金额，币种固定为港币）
CREATE TABLE IF NOT EXISTS payment_config (
    id SERIAL PRIMARY KEY,
    amount BIGINT NOT NULL DEFAULT 5900,
    currency VARCHAR(10) NOT NULL DEFAULT 'hkd',
    description VARCHAR(255) DEFAULT '支付配置',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uk_currency UNIQUE (currency)
);

-- 为 payment_config 表创建更新时间触发器
DROP TRIGGER IF EXISTS update_payment_config_updated_at ON payment_config;
CREATE TRIGGER update_payment_config_updated_at
    BEFORE UPDATE ON payment_config
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- 添加表注释
COMMENT ON TABLE payment_config IS '支付金额配置表';
COMMENT ON COLUMN payment_config.amount IS '支付金额（分），默认59港币';
COMMENT ON COLUMN payment_config.currency IS '币种，固定为港币';
COMMENT ON COLUMN payment_config.description IS '配置描述';

-- 初始化默认配置（59港币）
INSERT INTO payment_config (amount, currency, description) 
VALUES (5900, 'hkd', '默认支付金额配置')
ON CONFLICT (currency) DO UPDATE 
    SET amount = 5900, updated_at = CURRENT_TIMESTAMP;
