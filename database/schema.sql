-- Pay API 数据库结构
-- 创建数据库
CREATE DATABASE IF NOT EXISTS pay_api CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

USE pay_api;

-- 1. 支付历史表（存储所有支付记录）
CREATE TABLE IF NOT EXISTS payment_history (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    payment_intent_id VARCHAR(255) NOT NULL UNIQUE COMMENT 'Stripe PaymentIntent ID',
    payment_id VARCHAR(255) NOT NULL COMMENT '内部支付ID (UUID)',
    idempotency_key VARCHAR(255) NULL COMMENT '幂等性密钥，用于防止重复请求',
    user_id VARCHAR(255) NOT NULL COMMENT '用户ID',
    amount BIGINT UNSIGNED NOT NULL COMMENT '支付金额（分）',
    currency VARCHAR(10) NOT NULL DEFAULT 'hkd' COMMENT '币种',
    status VARCHAR(50) NOT NULL COMMENT '支付状态: succeeded, pending, failed, canceled等',
    payment_method VARCHAR(50) NOT NULL COMMENT '支付方式: card, wechat_pay, alipay, apple_pay等',
    description TEXT COMMENT '支付描述',
    metadata JSON COMMENT '额外元数据（JSON格式）',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    INDEX idx_user_id (user_id),
    INDEX idx_payment_intent_id (payment_intent_id),
    INDEX idx_status (status),
    INDEX idx_created_at (created_at),
    UNIQUE INDEX uk_idempotency_key (idempotency_key) COMMENT '幂等性密钥唯一索引'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='支付历史记录表';

-- 2. 用户支付信息表（存储用户是否支付成功过）
CREATE TABLE IF NOT EXISTS user_payment_info (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    user_id VARCHAR(255) NOT NULL UNIQUE COMMENT '用户ID',
    has_paid BOOLEAN NOT NULL DEFAULT FALSE COMMENT '是否支付成功过',
    first_payment_at TIMESTAMP NULL COMMENT '首次支付成功时间',
    last_payment_at TIMESTAMP NULL COMMENT '最近一次支付成功时间',
    total_payment_count INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '总支付成功次数',
    total_payment_amount BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '累计支付金额（分）',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    INDEX idx_user_id (user_id),
    INDEX idx_has_paid (has_paid)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户支付信息表';

-- 3. 支付金额配置表（存储需要支付的金额，币种固定为港币）
CREATE TABLE IF NOT EXISTS payment_config (
    id INT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    amount BIGINT UNSIGNED NOT NULL DEFAULT 5900 COMMENT '支付金额（分），默认59港币',
    currency VARCHAR(10) NOT NULL DEFAULT 'hkd' COMMENT '币种，固定为港币',
    description VARCHAR(255) DEFAULT '支付配置' COMMENT '配置描述',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
    UNIQUE KEY uk_currency (currency) COMMENT '币种唯一索引，确保每种币种只有一条配置'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='支付金额配置表';

-- 初始化默认配置（59港币）
INSERT INTO payment_config (amount, currency, description) 
VALUES (5900, 'hkd', '默认支付金额配置')
ON DUPLICATE KEY UPDATE amount = 5900, updated_at = CURRENT_TIMESTAMP;

