# Stripe Pay API

A Go backend service supporting Stripe payments and Apple in-app purchases.

## Features

- ✅ Stripe payment integration
- ✅ Apple App Store in-app purchase verification
- ✅ Webhook support
- ✅ RESTful API
- ✅ Containerized deployment support

## Quick Start

### Requirements

- Go 1.21+
- Docker & Docker Compose (optional)

### Local Development

1. Install dependencies
```bash
go mod download
```

2. Configure environment variables
```bash
cp env.example .env
# Edit the .env file and fill in your Stripe and Apple configuration
```

3. Start the service
```bash
go run main.go
```

The service will start at `http://localhost:8080`

### Docker Deployment

```bash
docker-compose up -d
```

## API Documentation

### Health Check

```bash
GET /ping
```

### Stripe Payment

#### Create Payment

```bash
POST /api/v1/stripe/create-payment
Content-Type: application/json

{
  "user_id": "user123",
  "description": "Test payment"
}
```

Response:
```json
{
  "client_secret": "pi_xxx_secret_xxx",
  "payment_id": "uuid",
  "payment_intent_id": "pi_xxx"
}
```

#### Confirm Payment

```bash
POST /api/v1/stripe/confirm-payment
Content-Type: application/json

{
  "payment_id": "uuid"
}
```

#### Webhook

```bash
POST /api/v1/stripe/webhook
```

### Apple In-App Purchase

#### Verify Purchase

```bash
POST /api/v1/apple/verify
Content-Type: application/json

{
  "receipt_data": "base64_receipt_data",
  "password": "optional_shared_secret"
}
```

#### Verify Subscription

```bash
POST /api/v1/apple/verify-subscription
Content-Type: application/json

{
  "receipt_data": "base64_receipt_data"
}
```

### Query Payment Status

```bash
GET /api/v1/payment/status/:id
```

## Configuration

### config.yaml

```yaml
server:
  port: 8080
  host: 0.0.0.0

stripe:
  secret_key: ""  # Stripe secret key
  webhook_secret: ""  # Webhook secret

apple:
  shared_secret: ""  # App Store shared secret
  production_url: "https://buy.itunes.apple.com/verifyReceipt"
  sandbox_url: "https://sandbox.itunes.apple.com/verifyReceipt"

log:
  level: "info"
```

### Environment Variables

- `STRIPE_SECRET_KEY`: Stripe Secret Key
- `STRIPE_WEBHOOK_SECRET`: Stripe Webhook Secret
- `APPLE_SHARED_SECRET`: Apple Shared Secret

## Development

### Project Structure

```
stripe-pay/
├── main.go           # Application entry point
├── config.yaml       # Configuration file
├── conf/             # Configuration management
│   └── config.go
├── biz/              # Business logic
│   ├── handlers/     # HTTP handlers
│   ├── services/     # Business services
│   └── models/       # Data models
├── cache/            # Redis cache
├── db/               # Database layer
├── common/           # Common utilities
├── go.mod
├── go.sum
├── Dockerfile
└── README.md
```

## Security

⚠️ **Important**: For production environments, please ensure:
- Use HTTPS
- Configure proper webhook signature verification
- Protect sensitive configuration information
- Implement appropriate authentication and authorization
- Use database to store payment status (not in-memory)

## License

MIT