# Stripe Pay API

A comprehensive Go backend service for payment processing with Stripe integration, supporting multiple payment methods including credit cards, Apple Pay, WeChat Pay, and Alipay. The service also includes Apple App Store in-app purchase verification capabilities.

## 🚀 Features

### Core Payment Features

- ✅ **Stripe Payment Integration**
  - Credit card payments via Stripe.js
  - Apple Pay support (Payment Request Button)
  - WeChat Pay integration
  - Alipay integration
  - Payment Intent creation and confirmation
  - Automatic payment method detection

- ✅ **Payment Management**
  - Dynamic pricing configuration (stored in database)
  - Payment status tracking and querying
  - Payment history management
  - User payment information tracking
  - Refund support (full and partial)

- ✅ **Apple App Store Integration**
  - In-app purchase receipt verification
  - Subscription verification
  - Webhook support for App Store Server Notifications

- ✅ **Advanced Features**
  - **Idempotency Support**: Prevent duplicate payments using `Idempotency-Key` header
  - **Smart Caching**: Redis-based caching with accuracy-first strategy
  - **Status Change Detection**: Real-time payment status change notifications
  - **Duplicate Payment Prevention**: Automatic check for already-paid users
  - **Comprehensive Logging**: Detailed request/response logging with stage tracking

### Technical Features

- ✅ **RESTful API** with comprehensive error handling
- ✅ **Database Persistence** (PostgreSQL) for payment records
- ✅ **Redis Caching** for performance optimization
- ✅ **Webhook Support** for Stripe and Apple events
- ✅ **CORS Support** for cross-origin requests
- ✅ **Request Logging** with request ID tracking
- ✅ **Error Recovery** with panic handling
- ✅ **Containerized Deployment** support (Docker)

## 📋 Table of Contents

- [Quick Start](#quick-start)
- [API Documentation](#api-documentation)
- [Configuration](#configuration)
- [Architecture](#architecture)
- [Features Details](#features-details)
- [Development](#development)
- [Security](#security)

## 🏃 Quick Start

### Requirements

- Go 1.21+
- PostgreSQL 12+ (推荐 PostgreSQL 14+)
- Redis 6.0+ (optional, for caching)
- Docker & Docker Compose (optional)

### Local Development

1. **Clone and install dependencies**
```bash
cd stripe-pay
go mod download
```

2. **Set up database**
```bash
cd database
./setup.sh
# Or use quick setup
./quick_setup.sh
```

3. **Set up Redis** (optional)
```bash
# Install Redis locally or use Docker
docker run -d -p 6379:6379 redis:latest
```

4. **Configure environment**
```bash
cp env.example .env
# Edit .env file with your configuration
```

5. **Create config.yaml**
```yaml
server:
  host: "0.0.0.0"
  port: "8080"

database:
  host: "localhost"
  port: 3306
  user: "root"
  password: "your_password"
  database: "payment_db"

redis:
  host: "localhost"
  port: 6379
  password: ""
  database: 0

stripe:
  secret_key: "sk_test_..."
  webhook_secret: "whsec_..."

apple:
  shared_secret: "your_shared_secret"
  production_url: "https://buy.itunes.apple.com/verifyReceipt"
  sandbox_url: "https://sandbox.itunes.apple.com/verifyReceipt"
```

6. **Start the service**
```bash
go run main.go
```

The service will start at `http://localhost:8080`

### Docker Deployment

```bash
docker-compose up -d
```

## 📚 API Documentation

### Base URL
- Local: `http://localhost:8080`
- Production: Your HTTPS domain

### Common Headers
- `Content-Type: application/json`
- `Idempotency-Key` (optional): For idempotency control
- `X-Request-ID` (optional): For request tracking

### API Endpoints

#### 1. Health Check
```
GET /ping
```
Returns: `{"message": "pong"}`

#### 2. Get Pricing
```
GET /api/v1/pricing
```
Returns current payment pricing from database.

**Response:**
```json
{
  "amount": 5900,
  "currency": "hkd",
  "label": "HK$59"
}
```

#### 3. Create Stripe Payment
```
POST /api/v1/stripe/create-payment
```
Creates a Stripe Payment Intent for credit card or Apple Pay.

**Request:**
```json
{
  "user_id": "user_12345",
  "description": "Payment description"
}
```

**Response:**
```json
{
  "client_secret": "pi_xxx_secret_xxx",
  "payment_id": "uuid",
  "payment_intent_id": "pi_xxx"
}
```

**Special Response (Already Paid):**
```json
{
  "already_paid": true,
  "message": "用户已支付成功，无需重复支付",
  "user_info": {...},
  "days_remaining": 30
}
```

#### 4. Create WeChat Pay Payment
```
POST /api/v1/stripe/create-wechat-payment
```
Creates a WeChat Pay payment intent.

**Request:**
```json
{
  "user_id": "user_12345",
  "description": "Payment description",
  "return_url": "https://example.com/return",
  "client": "web"
}
```

**Response:**
```json
{
  "client_secret": "pi_xxx_secret_xxx",
  "payment_intent_id": "pi_xxx",
  "status": "requires_action",
  "next_action": {
    "type": "wechat_pay_display_qr_code",
    "wechat_pay_display_qr_code": {
      "image_data_url": "data:image/png;base64,..."
    }
  }
}
```

#### 5. Create Alipay Payment
```
POST /api/v1/stripe/create-alipay-payment
```
Creates an Alipay payment intent.

**Request:**
```json
{
  "user_id": "user_12345",
  "description": "Payment description",
  "return_url": "https://example.com/return"
}
```

#### 6. Confirm Payment
```
POST /api/v1/stripe/confirm-payment
```
Confirms payment status by querying Stripe.

**Request:**
```json
{
  "payment_id": "pi_xxx"
}
```

#### 7. Query Payment Status
```
GET /api/v1/payment/status/:id
```
Query payment status by `payment_id` (UUID) or `payment_intent_id` (Stripe ID starting with `pi_`).

**Response:**
```json
{
  "payment_id": "uuid",
  "payment_intent_id": "pi_xxx",
  "status": "succeeded",
  "amount": 5900,
  "currency": "hkd",
  "source": "stripe",
  "cached": false
}
```

**Status Values:**
- `requires_payment_method`: Waiting for payment method
- `requires_confirmation`: Waiting for confirmation
- `requires_action`: Requires additional action (3D Secure, etc.)
- `processing`: Payment is being processed
- `succeeded`: Payment succeeded
- `canceled`: Payment canceled
- `requires_capture`: Waiting for capture

#### 8. Check Payment Status Change
```
GET /api/v1/payment/status-change/:payment_intent_id
```
Check if payment status has changed (for polling).

**Response (Status Changed):**
```json
{
  "payment_intent_id": "pi_xxx",
  "status_changed": true,
  "old_status": "processing",
  "new_status": "succeeded",
  "changed_at": "2025-01-01T12:00:00Z",
  "source": "webhook"
}
```

#### 9. Update Payment Status
```
POST /api/v1/payment/update-status
```
Update payment status after frontend payment success.

**Request:**
```json
{
  "payment_intent_id": "pi_xxx",
  "status": "succeeded"
}
```

#### 10. Get User Payment Info
```
GET /api/v1/user/:user_id/payment-info
```
Get user's payment information.

**Response:**
```json
{
  "user_id": "user_12345",
  "has_paid": true,
  "first_payment_at": "2025-01-01T00:00:00Z",
  "last_payment_at": "2025-01-15T00:00:00Z",
  "total_payment_count": 2,
  "total_payment_amount": 11800
}
```

#### 11. Get User Payment History
```
GET /api/v1/user/:user_id/payment-history?limit=50
```
Get user's payment history.

**Response:**
```json
{
  "user_id": "user_12345",
  "count": 2,
  "history": [...]
}
```

#### 12. Refund Payment
```
POST /api/v1/stripe/refund
```
Refund a payment (full or partial).

**Request:**
```json
{
  "payment_intent_id": "pi_xxx",
  "amount": 2000,
  "reason": "requested_by_customer"
}
```

#### 13. Get Payment Config
```
GET /api/v1/payment/config?currency=hkd
```
Get payment configuration (admin interface).

#### 14. Update Payment Config
```
PUT /api/v1/payment/config
```
Update payment configuration (admin interface).

**Request:**
```json
{
  "amount": 5900,
  "currency": "hkd",
  "description": "Payment description"
}
```

#### 15. Apple In-App Purchase Verification
```
POST /api/v1/apple/verify
```
Verify Apple App Store in-app purchase receipt.

**Request:**
```json
{
  "receipt_data": "base64_receipt_data",
  "password": "optional_shared_secret"
}
```

#### 16. Apple Subscription Verification
```
POST /api/v1/apple/verify-subscription
```
Verify Apple subscription receipt.

#### 17. Stripe Webhook
```
POST /api/v1/stripe/webhook
```
Receive Stripe webhook events (server-side only).

**Required Headers:**
- `Stripe-Signature`: Stripe webhook signature

**Supported Events:**
- `payment_intent.succeeded`
- `payment_intent.payment_failed`
- `payment_intent.canceled`

#### 18. Apple Webhook
```
POST /api/v1/apple/webhook
```
Receive Apple App Store Server Notifications (server-side only).

## ⚙️ Configuration

### config.yaml

```yaml
server:
  host: "0.0.0.0"
  port: "8080"

database:
  host: "localhost"
  port: 3306
  user: "root"
  password: "your_password"
  database: "payment_db"

redis:
  host: "localhost"
  port: 6379
  password: ""
  database: 0

stripe:
  secret_key: "sk_test_..."  # Stripe secret key
  webhook_secret: "whsec_..."  # Webhook secret

apple:
  shared_secret: "your_shared_secret"
  production_url: "https://buy.itunes.apple.com/verifyReceipt"
  sandbox_url: "https://sandbox.itunes.apple.com/verifyReceipt"

log:
  level: "info"  # debug, info, warn, error
```

### Environment Variables

You can also use environment variables (they override config.yaml):

- `STRIPE_SECRET_KEY`: Stripe Secret Key
- `STRIPE_WEBHOOK_SECRET`: Stripe Webhook Secret
- `APPLE_SHARED_SECRET`: Apple Shared Secret
- `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`: Database configuration
- `REDIS_HOST`, `REDIS_PORT`, `REDIS_PASSWORD`: Redis configuration

## 🏗️ Architecture

### Project Structure

```
stripe-pay/
├── main.go                 # Application entry point
├── config.yaml             # Configuration file
├── go.mod                  # Go dependencies
├── go.sum
├── Dockerfile              # Docker image definition
├── docker-compose.yml      # Docker Compose configuration
│
├── conf/                   # Configuration management
│   └── config.go
│
├── biz/                    # Business logic
│   ├── handlers/          # HTTP handlers
│   │   └── payment_handler.go
│   ├── services/          # Business services
│   │   └── payment_service.go
│   ├── models/            # Data models
│   │   └── payment.go
│   └── validation.go      # Input validation
│
├── db/                     # Database layer
│   ├── db.go              # Database connection
│   └── payment.go         # Payment data access
│
├── cache/                  # Redis cache
│   └── redis.go
│
├── common/                 # Common utilities
│   ├── errors.go          # Error handling
│   ├── response.go        # Response helpers
│   └── logger.go          # Logging utilities
│
└── database/               # Database setup scripts
    ├── schema.sql
    ├── init.sql
    └── setup.sh
```

### Technology Stack

- **Framework**: CloudWeGo Hertz (high-performance HTTP framework)
- **Database**: PostgreSQL (for persistent storage)
- **Cache**: Redis (for performance optimization)
- **Payment**: Stripe API
- **Logging**: Zap (structured logging)
- **Validation**: Custom validation layer

## 🔍 Features Details

### 1. Idempotency Support

All payment creation endpoints support idempotency via `Idempotency-Key` header:

```bash
curl -X POST http://localhost:8080/api/v1/stripe/create-payment \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: unique-key-123" \
  -d '{"user_id": "user_123", "description": "test"}'
```

If the same key is used again, the existing payment record is returned instead of creating a new one.

### 2. Smart Caching Strategy

The service uses an **accuracy-first caching strategy**:

- **Final Statuses** (`succeeded`, `canceled`): Always queried from Stripe in real-time, not cached
- **Intermediate Statuses** (`processing`, `requires_action`): Cached with TTL, but validated in background
- **Stale-While-Revalidate**: Returns cached data immediately, validates in background

### 3. Status Change Detection

The service tracks payment status changes and notifies clients:

- Webhook events update status and trigger change events
- Clients can poll `GET /api/v1/payment/status-change/:payment_intent_id` to detect changes
- Change events are stored in Redis with TTL

### 4. Duplicate Payment Prevention

The service automatically checks if a user has already paid:

- Checks user payment validity (30-day window)
- Returns `already_paid: true` if user has valid payment
- Prevents unnecessary payment creation

### 5. Comprehensive Logging

Every request is logged with:

- Request start/end timestamps
- Request ID for tracking
- Processing stages (binding, validation, business logic, etc.)
- Service-level logs with detailed context
- Error logs with stack traces

**Example Log Output:**
```
INFO    Request started                    method=POST path=/api/v1/stripe/create-payment request_id=REQ-...
INFO    Processing stage                   stage=request_received handler=CreateStripePayment
INFO    Processing stage                   stage=validation_passed
INFO    Service: CreateStripePayment started user_id=user123
INFO    Service: Stripe PaymentIntent created payment_intent_id=pi_xxx
INFO    Request completed                  status_code=200 latency=150ms
```

### 6. Error Handling

All errors follow a unified format:

```json
{
  "code": 400,
  "message": "Validation failed",
  "details": "user_id is required",
  "error_id": "ERR-1234567890"
}
```

- **400**: Client errors (validation, missing parameters)
- **404**: Resource not found
- **500**: Server errors
- **Error ID**: For log tracking and debugging

## 💻 Development

### Running Tests

```bash
go test ./...
```

### Building

```bash
go build -o stripe-pay main.go
```

### Code Structure

- **Handlers**: HTTP request handling, validation, response formatting
- **Services**: Business logic, external API calls (Stripe, Apple)
- **DB Layer**: Database operations, data models
- **Cache Layer**: Redis operations, caching strategies
- **Common**: Shared utilities (errors, logging, responses)

### Adding New Features

1. Add handler in `biz/handlers/`
2. Add service method in `biz/services/`
3. Add database operations in `db/`
4. Add route in `main.go`
5. Update API documentation

## 🔒 Security

### Production Checklist

- ✅ **Use HTTPS**: Always use HTTPS in production
- ✅ **Webhook Signature Verification**: Stripe webhooks are verified using signatures
- ✅ **Environment Variables**: Store sensitive keys in environment variables, not in code
- ✅ **Database Security**: Use strong passwords, restrict access
- ✅ **Redis Security**: Use password authentication for Redis
- ✅ **Input Validation**: All inputs are validated before processing
- ✅ **Error Sanitization**: Sensitive information is removed from error messages in production
- ✅ **CORS Configuration**: Configure CORS properly for your frontend domain
- ✅ **Rate Limiting**: Consider adding rate limiting for production (not included by default)

### Best Practices

1. **Never commit secrets**: Use `.env` files and add them to `.gitignore`
2. **Use test keys for development**: Use Stripe test keys (`sk_test_...`) for development
3. **Monitor logs**: Regularly check logs for suspicious activity
4. **Update dependencies**: Keep dependencies up to date
5. **Database backups**: Regular backups of payment data
6. **Webhook security**: Always verify webhook signatures

## 📝 License

MIT

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## 📞 Support

For issues and questions, please open an issue on the repository.

---

**Note**: This service is designed for production use but requires proper security configuration. Always follow security best practices when deploying to production.
