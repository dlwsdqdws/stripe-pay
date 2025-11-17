package db

import (
	"database/sql"
	"fmt"
	"stripe-pay/conf"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
)

var DB *sql.DB

// Init 初始化数据库连接
func Init() error {
	cfg := conf.GetConf()

	// 构建 DSN (Data Source Name)
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.Database,
		cfg.Database.Charset,
	)

	var err error
	DB, err = sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// 设置连接池参数
	DB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	DB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	DB.SetConnMaxLifetime(time.Duration(cfg.Database.ConnMaxLifetime) * time.Second)

	// 测试连接
	if err = DB.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	// 检查数据库结构（验证迁移是否完成）
	if err = checkDatabaseSchema(); err != nil {
		return fmt.Errorf("database schema check failed: %w", err)
	}

	zap.L().Info("Database connected successfully",
		zap.String("host", cfg.Database.Host),
		zap.Int("port", cfg.Database.Port),
		zap.String("database", cfg.Database.Database))

	return nil
}

// checkDatabaseSchema 检查数据库结构，确保必要的字段和索引存在
func checkDatabaseSchema() error {
	// 检查 idempotency_key 字段是否存在
	var columnExists int
	query := `SELECT COUNT(*) 
		FROM INFORMATION_SCHEMA.COLUMNS 
		WHERE table_schema = DATABASE() 
		  AND table_name = 'payment_history' 
		  AND column_name = 'idempotency_key'`

	err := DB.QueryRow(query).Scan(&columnExists)
	if err != nil {
		return fmt.Errorf("failed to check idempotency_key column: %w", err)
	}

	if columnExists == 0 {
		cfg := conf.GetConf()
		return fmt.Errorf("database migration required: idempotency_key column does not exist. Please run: mysql -u %s -p %s < database/add_idempotency_key.sql (or check config.yaml for your database user)", cfg.Database.User, cfg.Database.Database)
	}

	// 检查唯一索引是否存在
	var indexExists int
	query = `SELECT COUNT(*) 
		FROM INFORMATION_SCHEMA.STATISTICS 
		WHERE table_schema = DATABASE() 
		  AND table_name = 'payment_history' 
		  AND index_name = 'uk_idempotency_key'`

	err = DB.QueryRow(query).Scan(&indexExists)
	if err != nil {
		return fmt.Errorf("failed to check uk_idempotency_key index: %w", err)
	}

	if indexExists == 0 {
		cfg := conf.GetConf()
		return fmt.Errorf("database migration required: uk_idempotency_key index does not exist. Please run: mysql -u %s -p %s < database/add_idempotency_key.sql (or check config.yaml for your database user)", cfg.Database.User, cfg.Database.Database)
	}

	zap.L().Info("Database schema check passed: idempotency_key column and index exist")
	return nil
}

// Close 关闭数据库连接
func Close() error {
	if DB != nil {
		return DB.Close()
	}
	return nil
}
