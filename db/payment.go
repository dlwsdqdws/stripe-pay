package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"stripe-pay/conf"
	"strings"
	"time"

	"go.uber.org/zap"
)

// DuplicateIdempotencyKeyError 表示idempotency_key重复的错误
type DuplicateIdempotencyKeyError struct {
	Key string
}

func (e *DuplicateIdempotencyKeyError) Error() string {
	return fmt.Sprintf("duplicate idempotency_key: %s", e.Key)
}

// PaymentHistory 支付历史记录
type PaymentHistory struct {
	ID              int64     `json:"id"`
	PaymentIntentID string    `json:"payment_intent_id"`
	PaymentID       string    `json:"payment_id"`
	IdempotencyKey  string    `json:"idempotency_key"` // 幂等性密钥
	UserID          string    `json:"user_id"`
	Amount          int64     `json:"amount"`
	Currency        string    `json:"currency"`
	Status          string    `json:"status"`
	PaymentMethod   string    `json:"payment_method"`
	Description     string    `json:"description"`
	Metadata        string    `json:"metadata"` // JSON 字符串
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// UserPaymentInfo 用户支付信息
type UserPaymentInfo struct {
	ID                 int64      `json:"id"`
	UserID             string     `json:"user_id"`
	HasPaid            bool       `json:"has_paid"`
	FirstPaymentAt     *time.Time `json:"first_payment_at"`
	LastPaymentAt      *time.Time `json:"last_payment_at"`
	TotalPaymentCount  int        `json:"total_payment_count"`
	TotalPaymentAmount int64      `json:"total_payment_amount"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// SavePaymentHistory 保存支付历史记录
func SavePaymentHistory(ph *PaymentHistory) error {
	// PostgreSQL: 使用 ON CONFLICT 处理 payment_intent_id 或 idempotency_key 的冲突
	query := `INSERT INTO payment_history 
		(payment_intent_id, payment_id, idempotency_key, user_id, amount, currency, status, payment_method, description, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (payment_intent_id) DO UPDATE
			SET status = EXCLUDED.status,
				updated_at = CURRENT_TIMESTAMP
		RETURNING id`

	metadataJSON := ""
	if ph.Metadata != "" {
		metadataJSON = ph.Metadata
	}

	err := DB.QueryRow(query,
		ph.PaymentIntentID,
		ph.PaymentID,
		ph.IdempotencyKey,
		ph.UserID,
		ph.Amount,
		ph.Currency,
		ph.Status,
		ph.PaymentMethod,
		ph.Description,
		metadataJSON,
	).Scan(&ph.ID)

	if err != nil {
		// 检查是否是字段不存在的错误（数据库迁移未执行）
		if strings.Contains(err.Error(), "column") && strings.Contains(err.Error(), "does not exist") {
			cfg := conf.GetConf()
			zap.L().Error("Database migration required: idempotency_key column does not exist",
				zap.String("payment_intent_id", ph.PaymentIntentID),
				zap.String("error", err.Error()))
			return fmt.Errorf("database migration required: please run 'psql -U %s -d %s -f database/add_idempotency_key.sql' to add idempotency_key column (check config.yaml for your database user)", cfg.Database.User, cfg.Database.Database)
		}
		// 检查是否是唯一约束冲突（idempotency_key重复）
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "UNIQUE constraint") || strings.Contains(err.Error(), "unique constraint") {
			zap.L().Warn("Duplicate idempotency_key detected, payment may already exist",
				zap.String("idempotency_key", ph.IdempotencyKey),
				zap.String("payment_intent_id", ph.PaymentIntentID))
			// 返回特殊错误，让调用者知道是重复请求
			return &DuplicateIdempotencyKeyError{Key: ph.IdempotencyKey}
		}
		zap.L().Error("Failed to save payment history", zap.Error(err))
		return err
	}

	zap.L().Info("Payment history saved", zap.Int64("id", ph.ID), zap.String("payment_intent_id", ph.PaymentIntentID))

	return nil
}

// UpdatePaymentStatus 更新支付状态
func UpdatePaymentStatus(paymentIntentID, status string) error {
	query := `UPDATE payment_history 
		SET status = $1, updated_at = CURRENT_TIMESTAMP 
		WHERE payment_intent_id = $2`

	_, err := DB.Exec(query, status, paymentIntentID)
	if err != nil {
		zap.L().Error("Failed to update payment status", zap.Error(err), zap.String("payment_intent_id", paymentIntentID))
		return err
	}

	zap.L().Info("Payment status updated", zap.String("payment_intent_id", paymentIntentID), zap.String("status", status))
	return nil
}

// UpdateUserPaymentInfo 更新用户支付信息（支付成功时调用）
func UpdateUserPaymentInfo(userID string, amount int64) error {
	now := time.Now()

	// 先检查用户是否存在
	var exists bool
	err := DB.QueryRow("SELECT EXISTS(SELECT 1 FROM user_payment_info WHERE user_id = $1)", userID).Scan(&exists)
	if err != nil {
		zap.L().Error("Failed to check user payment info", zap.Error(err))
		return err
	}

	if !exists {
		// 插入新记录
		query := `INSERT INTO user_payment_info 
			(user_id, has_paid, first_payment_at, last_payment_at, total_payment_count, total_payment_amount)
			VALUES ($1, true, $2, $3, 1, $4)`

		_, err = DB.Exec(query, userID, now, now, amount)
		if err != nil {
			zap.L().Error("Failed to insert user payment info", zap.Error(err))
			return err
		}
	} else {
		// 更新现有记录
		// 先检查是否是首次支付
		var firstPaymentAt sql.NullTime
		err = DB.QueryRow("SELECT first_payment_at FROM user_payment_info WHERE user_id = $1", userID).Scan(&firstPaymentAt)

		// 如果是首次支付，更新首次支付时间
		if err == nil && (!firstPaymentAt.Valid || firstPaymentAt.Time.IsZero()) {
			query := `UPDATE user_payment_info 
				SET has_paid = true,
					first_payment_at = $1,
					last_payment_at = $2,
					total_payment_count = total_payment_count + 1,
					total_payment_amount = total_payment_amount + $3,
					updated_at = CURRENT_TIMESTAMP
				WHERE user_id = $4`
			_, err = DB.Exec(query, now, now, amount, userID)
		} else {
			// 非首次支付，只更新最近支付时间和统计
			query := `UPDATE user_payment_info 
				SET has_paid = true,
					last_payment_at = $1,
					total_payment_count = total_payment_count + 1,
					total_payment_amount = total_payment_amount + $2,
					updated_at = CURRENT_TIMESTAMP
				WHERE user_id = $3`
			_, err = DB.Exec(query, now, amount, userID)
		}

		if err != nil {
			zap.L().Error("Failed to update user payment info", zap.Error(err))
			return err
		}
	}

	zap.L().Info("User payment info updated", zap.String("user_id", userID))
	return nil
}

// GetUserPaymentInfo 获取用户支付信息（实时从 payment_history 查询，确保准确性）
func GetUserPaymentInfo(userID string) (*UserPaymentInfo, error) {
	// 从 payment_history 实时查询成功的支付记录
	query := `SELECT 
		COUNT(*) as total_count,
		COALESCE(SUM(amount), 0) as total_amount,
		MIN(created_at) as first_payment,
		MAX(created_at) as last_payment
		FROM payment_history 
		WHERE user_id = $1 AND status = 'succeeded'`

	var totalCount int
	var totalAmount int64
	var firstPayment, lastPayment sql.NullTime

	err := DB.QueryRow(query, userID).Scan(&totalCount, &totalAmount, &firstPayment, &lastPayment)
	if err != nil && err != sql.ErrNoRows {
		zap.L().Error("Failed to query payment history for user", zap.Error(err), zap.String("user_id", userID))
		return nil, err
	}

	// 构建用户支付信息
	info := &UserPaymentInfo{
		UserID:             userID,
		HasPaid:            totalCount > 0,
		TotalPaymentCount:  totalCount,
		TotalPaymentAmount: totalAmount,
	}

	if firstPayment.Valid {
		info.FirstPaymentAt = &firstPayment.Time
	}
	if lastPayment.Valid {
		info.LastPaymentAt = &lastPayment.Time
	}

	// 修复：查询接口不应该更新数据库，只应该读取
	// UpdateUserPaymentInfo 应该只在支付成功时调用（Webhook 或前端更新状态时）
	// 传入的应该是单次支付金额，而不是总金额
	// 删除这里的异步更新逻辑，避免重复累加和篡改账目

	return info, nil
}

// GetPaymentHistory 获取支付历史（按用户ID）
func GetPaymentHistory(userID string, limit int) ([]PaymentHistory, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `SELECT id, payment_intent_id, payment_id, idempotency_key, user_id, amount, currency, 
		status, payment_method, description, metadata, created_at, updated_at
		FROM payment_history 
		WHERE user_id = $1 
		ORDER BY created_at DESC 
		LIMIT $2`

	rows, err := DB.Query(query, userID, limit)
	if err != nil {
		zap.L().Error("Failed to query payment history", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	var history []PaymentHistory
	for rows.Next() {
		var ph PaymentHistory
		err := rows.Scan(
			&ph.ID,
			&ph.PaymentIntentID,
			&ph.PaymentID,
			&ph.IdempotencyKey,
			&ph.UserID,
			&ph.Amount,
			&ph.Currency,
			&ph.Status,
			&ph.PaymentMethod,
			&ph.Description,
			&ph.Metadata,
			&ph.CreatedAt,
			&ph.UpdatedAt,
		)
		if err != nil {
			zap.L().Error("Failed to scan payment history", zap.Error(err))
			continue
		}
		history = append(history, ph)
	}

	return history, nil
}

// GetPaymentByIdempotencyKey 根据幂等性密钥获取支付记录
func GetPaymentByIdempotencyKey(idempotencyKey string) (*PaymentHistory, error) {
	if idempotencyKey == "" {
		return nil, nil
	}

	// 先检查字段是否存在（处理数据库迁移未执行的情况）
	// 如果字段不存在，查询会失败，但我们不想因为这个阻止请求
	query := `SELECT id, payment_intent_id, payment_id, idempotency_key, user_id, amount, currency, 
		status, payment_method, description, metadata, created_at, updated_at
		FROM payment_history 
		WHERE idempotency_key = $1 
		LIMIT 1`

	ph := &PaymentHistory{}
	err := DB.QueryRow(query, idempotencyKey).Scan(
		&ph.ID,
		&ph.PaymentIntentID,
		&ph.PaymentID,
		&ph.IdempotencyKey,
		&ph.UserID,
		&ph.Amount,
		&ph.Currency,
		&ph.Status,
		&ph.PaymentMethod,
		&ph.Description,
		&ph.Metadata,
		&ph.CreatedAt,
		&ph.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		zap.L().Debug("No payment found with idempotency_key", zap.String("idempotency_key", idempotencyKey))
		return nil, nil
	}

	if err != nil {
		// 检查是否是字段不存在的错误
		if strings.Contains(err.Error(), "column") && strings.Contains(err.Error(), "does not exist") {
			zap.L().Warn("idempotency_key column may not exist, database migration may be needed",
				zap.String("idempotency_key", idempotencyKey),
				zap.Error(err))
			// 返回nil而不是错误，让请求继续执行（向后兼容）
			return nil, nil
		}
		zap.L().Error("Failed to get payment by idempotency key", zap.Error(err), zap.String("idempotency_key", idempotencyKey))
		return nil, err
	}

	zap.L().Info("Found existing payment by idempotency_key",
		zap.String("idempotency_key", idempotencyKey),
		zap.String("payment_intent_id", ph.PaymentIntentID))
	return ph, nil
}

// GetPaymentByPaymentID 根据payment_id获取支付记录
func GetPaymentByPaymentID(paymentID string) (*PaymentHistory, error) {
	if paymentID == "" {
		return nil, nil
	}

	query := `SELECT id, payment_intent_id, payment_id, idempotency_key, user_id, amount, currency, 
		status, payment_method, description, metadata, created_at, updated_at
		FROM payment_history 
		WHERE payment_id = $1 
		LIMIT 1`

	ph := &PaymentHistory{}
	err := DB.QueryRow(query, paymentID).Scan(
		&ph.ID,
		&ph.PaymentIntentID,
		&ph.PaymentID,
		&ph.IdempotencyKey,
		&ph.UserID,
		&ph.Amount,
		&ph.Currency,
		&ph.Status,
		&ph.PaymentMethod,
		&ph.Description,
		&ph.Metadata,
		&ph.CreatedAt,
		&ph.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		zap.L().Debug("No payment found with payment_id", zap.String("payment_id", paymentID))
		return nil, nil
	}

	if err != nil {
		zap.L().Error("Failed to get payment by payment_id", zap.Error(err), zap.String("payment_id", paymentID))
		return nil, err
	}

	zap.L().Info("Found payment by payment_id",
		zap.String("payment_id", paymentID),
		zap.String("payment_intent_id", ph.PaymentIntentID))
	return ph, nil
}

// GetPaymentByIntentID 根据payment_intent_id获取支付记录
func GetPaymentByIntentID(paymentIntentID string) (*PaymentHistory, error) {
	if paymentIntentID == "" {
		return nil, nil
	}

	query := `SELECT id, payment_intent_id, payment_id, idempotency_key, user_id, amount, currency, 
		status, payment_method, description, metadata, created_at, updated_at
		FROM payment_history 
		WHERE payment_intent_id = $1 
		LIMIT 1`

	ph := &PaymentHistory{}
	err := DB.QueryRow(query, paymentIntentID).Scan(
		&ph.ID,
		&ph.PaymentIntentID,
		&ph.PaymentID,
		&ph.IdempotencyKey,
		&ph.UserID,
		&ph.Amount,
		&ph.Currency,
		&ph.Status,
		&ph.PaymentMethod,
		&ph.Description,
		&ph.Metadata,
		&ph.CreatedAt,
		&ph.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		zap.L().Debug("No payment found with payment_intent_id", zap.String("payment_intent_id", paymentIntentID))
		return nil, nil
	}

	if err != nil {
		zap.L().Error("Failed to get payment by payment_intent_id", zap.Error(err), zap.String("payment_intent_id", paymentIntentID))
		return nil, err
	}

	zap.L().Info("Found payment by payment_intent_id",
		zap.String("payment_intent_id", paymentIntentID),
		zap.String("payment_id", ph.PaymentID))
	return ph, nil
}

// SavePaymentWithMetadata 保存支付记录（带元数据）
func SavePaymentWithMetadata(paymentIntentID, paymentID, idempotencyKey, userID string, amount int64, currency, status, paymentMethod, description string, metadata map[string]string) error {
	metadataJSON := ""
	if len(metadata) > 0 {
		bytes, err := json.Marshal(metadata)
		if err == nil {
			metadataJSON = string(bytes)
		}
	}

	ph := &PaymentHistory{
		PaymentIntentID: paymentIntentID,
		PaymentID:       paymentID,
		IdempotencyKey:  idempotencyKey,
		UserID:          userID,
		Amount:          amount,
		Currency:        currency,
		Status:          status,
		PaymentMethod:   paymentMethod,
		Description:     description,
		Metadata:        metadataJSON,
	}

	return SavePaymentHistory(ph)
}

// PaymentConfig 支付金额配置
type PaymentConfig struct {
	ID          int       `json:"id"`
	Amount      int64     `json:"amount"`      // 金额（分）
	Currency    string    `json:"currency"`    // 币种
	Description string    `json:"description"` // 描述
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// GetPaymentConfig 获取支付金额配置（按币种，默认 hkd）
func GetPaymentConfig(currency string) (*PaymentConfig, error) {
	if currency == "" {
		currency = "hkd"
	}

	query := `SELECT id, amount, currency, description, created_at, updated_at
		FROM payment_config 
		WHERE currency = $1 
		LIMIT 1`

	config := &PaymentConfig{}
	err := DB.QueryRow(query, currency).Scan(
		&config.ID,
		&config.Amount,
		&config.Currency,
		&config.Description,
		&config.CreatedAt,
		&config.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		// 如果不存在，返回默认值
		return &PaymentConfig{
			Amount:   5900,
			Currency: "hkd",
		}, nil
	}

	if err != nil {
		zap.L().Error("Failed to get payment config", zap.Error(err), zap.String("currency", currency))
		return nil, err
	}

	return config, nil
}

// UpdatePaymentConfig 更新支付金额配置
func UpdatePaymentConfig(currency string, amount int64, description string) error {
	if currency == "" {
		currency = "hkd"
	}

	// 使用 INSERT ... ON CONFLICT DO UPDATE 确保存在则更新，不存在则插入（PostgreSQL）
	query := `INSERT INTO payment_config (currency, amount, description, updated_at)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
		ON CONFLICT (currency) DO UPDATE
			SET amount = EXCLUDED.amount,
				description = EXCLUDED.description,
				updated_at = CURRENT_TIMESTAMP`

	_, err := DB.Exec(query, currency, amount, description)
	if err != nil {
		zap.L().Error("Failed to update payment config", zap.Error(err), zap.String("currency", currency), zap.Int64("amount", amount))
		return err
	}

	zap.L().Info("Payment config updated", zap.String("currency", currency), zap.Int64("amount", amount))
	return nil
}
