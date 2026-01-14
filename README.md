# AWS OpenSearch Proxy

A lightweight HTTP proxy for Amazon OpenSearch Service that automatically signs requests using AWS Signature Version 4.

## Features

- ✅ Automatic AWS request signing using AWS SDK v2
- ✅ Support for default AWS credential providers (EC2 instance profile, ECS task role, environment variables, etc.)
- ✅ Optional IAM role assumption for cross-account access
- ✅ Automatic credential refresh before expiration
- ✅ Health check endpoint
- ✅ Kubernetes-ready with manifests included
- ✅ Lightweight Docker image based on Alpine Linux
- ✅ Graceful shutdown handling

## How It Works

```
HTTP Client (curl/browser) → Proxy → Sign with AWS SigV4 → Amazon OpenSearch
                             ↓
                       AWS Credentials
                       (IAM Role/Keys)
```

The proxy:
1. Receives HTTP requests from clients
2. Signs them with AWS Signature Version 4 using provided credentials
3. Forwards signed requests to Amazon OpenSearch
4. Returns OpenSearch responses to the client

## Configuration

### Environment Variables

| Variable | Description | Default | Required |
|----------|-------------|---------|----------|
| `OPENSEARCH_URL` | Amazon OpenSearch endpoint URL | - | Yes |
| `AWS_REGION` | AWS region | `us-east-1` | No |
| `PORT` | Port to listen on | `8080` | No |
| `AWS_ASSUME_ROLE_ARN` | IAM role ARN to assume (for cross-account access) | - | No |
| `INSECURE_SKIP_TLS` | Skip TLS verification (not recommended for production) | `false` | No |

### Command Line Flags

```bash
./aws-opensearch-proxy \
  -opensearch-url https://search-domain.us-east-1.es.amazonaws.com \
  -region us-east-1 \
  -port 8080 \
  -assume-role arn:aws:iam::123456789012:role/opensearch-role
```

All flags have corresponding environment variables (see table above).

## Authentication

### Default Authentication

By default, the proxy uses the AWS SDK's default credential chain:
1. Environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`)
2. Web Identity Token (for EKS IRSA)
3. Shared credentials file (`~/.aws/credentials`)
4. ECS container credentials
5. EC2 instance profile credentials

### Cross-Account Access with Role Assumption

To access OpenSearch in a different AWS account:

1. Create an IAM role in the target account with OpenSearch permissions
2. Configure trust relationship to allow assumption from your account
3. Set `AWS_ASSUME_ROLE_ARN` environment variable or `-assume-role` flag

The proxy will:
- Assume the role using default credentials
- Request 1-hour session duration
- Automatically refresh credentials every 50 minutes

## Building

### Local Build

```bash
# Download dependencies
go mod download

# Format code (required before committing)
make fmt

# Build binary
make build

# Or manually:
go build -o aws-opensearch-proxy .
```

### Docker Build

```bash
docker build -t aws-opensearch-proxy:latest .
```

## Running Locally

### With Environment Variables

```bash
export OPENSEARCH_URL=https://search-domain.us-east-1.es.amazonaws.com
export AWS_REGION=us-east-1
export AWS_ASSUME_ROLE_ARN=arn:aws:iam::123456789012:role/opensearch-role

./aws-opensearch-proxy
```

### With Command Line Flags

```bash
./aws-opensearch-proxy \
  -opensearch-url https://search-domain.us-east-1.es.amazonaws.com \
  -region us-east-1 \
  -assume-role arn:aws:iam::123456789012:role/opensearch-role
```

## Testing

Once running, test the proxy:

```bash
# Health check
curl http://localhost:8080/_health

# OpenSearch cluster info
curl http://localhost:8080/

# Search request
curl -X GET "http://localhost:8080/my-index/_search" \
  -H "Content-Type: application/json" \
  -d '{"query": {"match_all": {}}}'

# Index a document
curl -X POST "http://localhost:8080/my-index/_doc" \
  -H "Content-Type: application/json" \
  -d '{"field": "value"}'
```

## Kubernetes Deployment

### Prerequisites

1. **IAM Role Setup** (for IRSA - IAM Roles for Service Accounts):

```bash
# Create IAM role for the service account
aws iam create-role \
  --role-name opensearch-proxy-role \
  --assume-role-policy-document '{
    "Version": "2012-10-17",
    "Statement": [{
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::ACCOUNT_ID:oidc-provider/oidc.eks.REGION.amazonaws.com/id/OIDC_ID"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "oidc.eks.REGION.amazonaws.com/id/OIDC_ID:sub": "system:serviceaccount:opensearch-proxy:aws-opensearch-proxy"
        }
      }
    }]
  }'

# Attach OpenSearch access policy
aws iam put-role-policy \
  --role-name opensearch-proxy-role \
  --policy-name opensearch-access \
  --policy-document '{
    "Version": "2012-10-17",
    "Statement": [{
      "Effect": "Allow",
      "Action": [
        "es:ESHttpGet",
        "es:ESHttpPost",
        "es:ESHttpPut",
        "es:ESHttpDelete",
        "es:ESHttpHead"
      ],
      "Resource": "arn:aws:es:REGION:ACCOUNT_ID:domain/DOMAIN_NAME/*"
    }]
  }'

# For cross-account role assumption, add this policy:
aws iam put-role-policy \
  --role-name opensearch-proxy-role \
  --policy-name assume-cross-account-role \
  --policy-document '{
    "Version": "2012-10-17",
    "Statement": [{
      "Effect": "Allow",
      "Action": "sts:AssumeRole",
      "Resource": "arn:aws:iam::TARGET_ACCOUNT_ID:role/cross-account-opensearch-role"
    }]
  }'
```

### Deployment Steps

1. **Update ConfigMap** (`configmap.yaml`):
   ```yaml
   data:
     OPENSEARCH_URL: "https://search-your-domain.us-east-1.es.amazonaws.com"
     AWS_REGION: "us-east-1"
     # Optional for cross-account:
     AWS_ASSUME_ROLE_ARN: "arn:aws:iam::TARGET_ACCOUNT:role/opensearch-role"
   ```

2. **Update ServiceAccount** (`serviceaccount.yaml`):
   ```yaml
   annotations:
     eks.amazonaws.com/role-arn: arn:aws:iam::ACCOUNT_ID:role/opensearch-proxy-role
   ```

3. **Update Deployment Image** (`deployment.yaml`):
   ```yaml
   image: your-registry/aws-opensearch-proxy:latest
   ```

4. **Apply Manifests**:
   ```bash
   kubectl apply -f namespace.yaml
   kubectl apply -f configmap.yaml
   kubectl apply -f serviceaccount.yaml
   kubectl apply -f deployment.yaml
   kubectl apply -f service.yaml
   kubectl apply -f ingress.yaml  # Optional, configure as needed
   ```

5. **Verify Deployment**:
   ```bash
   kubectl get pods -n opensearch-proxy
   kubectl logs -n opensearch-proxy -l app=aws-opensearch-proxy
   ```

### Access the Proxy

From within the cluster:
```bash
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl http://aws-opensearch-proxy.opensearch-proxy.svc.cluster.local/
```

From outside the cluster (if ingress is configured):
```bash
curl https://opensearch-proxy.example.com/
```

## Security Best Practices

1. **Use IRSA (IAM Roles for Service Accounts)** instead of storing AWS credentials
2. **Enable TLS** between clients and the proxy (configure ingress with TLS)
3. **Restrict network access** using Kubernetes NetworkPolicies
4. **Follow least privilege** - grant only necessary OpenSearch permissions
5. **Use separate roles** for different environments (dev, staging, prod)
6. **Enable audit logging** on your OpenSearch domain
7. **Run as non-root** (already configured in the Dockerfile and deployment)

## Troubleshooting

### Check Logs

```bash
kubectl logs -n opensearch-proxy -l app=aws-opensearch-proxy -f
```

### Common Issues

1. **"Failed to retrieve AWS credentials"**
   - Verify IRSA role ARN annotation on ServiceAccount
   - Check IAM role trust relationship
   - Verify pod has the correct service account attached

2. **"Failed to assume role"**
   - Check the assume role ARN is correct
   - Verify the base IAM role has `sts:AssumeRole` permission
   - Check target role's trust policy allows assumption

3. **"Failed to sign request"**
   - Verify AWS region is correct
   - Check OpenSearch domain endpoint URL

4. **OpenSearch returns 403 Forbidden**
   - Check IAM role has necessary OpenSearch permissions
   - Verify OpenSearch domain access policy
   - Check if domain requires VPC access

### Debug Mode

Run locally with verbose AWS SDK logging:
```bash
export AWS_SDK_LOAD_CONFIG=1
export AWS_LOG_LEVEL=debug
./aws-opensearch-proxy -opensearch-url YOUR_URL
```

## Performance Considerations

- **Connection Pooling**: The proxy maintains connection pools to OpenSearch (100 max idle connections)
- **Timeout**: Default request timeout is 30 seconds
- **Horizontal Scaling**: Scale by increasing replicas in the deployment
- **Resource Limits**: Adjust CPU/memory limits based on your workload

## Monitoring

### Metrics

The application logs:
- Request method, path, and status code
- Credential refresh events
- Error conditions

Example log output:
```
2026/01/13 10:00:00 Starting AWS OpenSearch proxy on port 8080
2026/01/13 10:00:00 Proxying to: https://search-domain.us-east-1.es.amazonaws.com
2026/01/13 10:00:00 Using assumed role: arn:aws:iam::123456789012:role/opensearch-role
2026/01/13 10:00:00 Successfully assumed role
2026/01/13 10:00:05 GET /_cat/indices 200
2026/01/13 10:50:00 Refreshing assumed role credentials...
2026/01/13 10:50:00 Credentials refreshed successfully
```

### Health Checks

- **Liveness Probe**: `GET /_health` (returns 200 OK)
- **Readiness Probe**: `GET /_health` (returns 200 OK)

## Docker Hub

Pre-built Docker images are available on Docker Hub:

```bash
docker pull jpellet/aws-opensearch-proxy:latest
docker pull jpellet/aws-opensearch-proxy:v1.0.0
```

Multi-architecture support:
- `linux/amd64` (x86_64)
- `linux/arm64` (ARM64/Apple Silicon)

## CI/CD

This project uses GitHub Actions for automated testing and Docker image builds:

- **Docker Publish**: Automatically builds and pushes multi-arch images to Docker Hub on version tags
- **Test**: Runs tests on every push and pull request
- **Docker Build Test**: Validates Docker builds on pull requests

See [`GITHUB_SETUP.md`](GITHUB_SETUP.md) for setup instructions.

## Version Information

Check the version of the proxy:

```bash
# Binary
./aws-opensearch-proxy -version

# Docker
docker run --rm jpellet/aws-opensearch-proxy:latest -version
```

## License

MIT - See [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

### Development Workflow

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes
4. **Format your code**: `make fmt`
5. **Run tests**: `make test`
6. **Run linters**: `make lint`
7. Commit your changes (`git commit -m 'Add some amazing feature'`)
8. Push to the branch (`git push origin feature/amazing-feature`)
9. Open a Pull Request

### Code Quality

Before committing, ensure:
- Code is formatted with `gofmt`: Run `make fmt`
- Tests pass: Run `make test`
- No linter errors: Run `make lint`

Optional: Install pre-commit hooks to automate checks:
```bash
pip install pre-commit
pre-commit install
```
