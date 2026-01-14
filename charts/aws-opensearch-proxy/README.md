# AWS OpenSearch Proxy Helm Chart

This Helm chart deploys the AWS OpenSearch Proxy with automatic AWS Signature V4 request signing.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.0+
- AWS EKS cluster with IRSA configured (recommended)
- IAM role with OpenSearch access permissions

## Installation

### Add the Helm repository (when published)

```bash
helm repo add aws-opensearch-proxy https://jpellet.github.io/aws-opensearch-proxy
helm repo update
```

### Install from local chart

```bash
helm install aws-opensearch-proxy ./charts/aws-opensearch-proxy \
  --set opensearch.url=https://search-domain.us-east-1.es.amazonaws.com \
  --set opensearch.region=us-east-1 \
  --set serviceAccount.annotations."eks\.amazonaws\.com/role-arn"=arn:aws:iam::ACCOUNT_ID:role/opensearch-proxy-role
```

### Install with custom values file

```bash
# Create values file
cat > my-values.yaml <<EOF
opensearch:
  url: "https://search-your-domain.us-east-1.es.amazonaws.com"
  region: "us-east-1"

serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: "arn:aws:iam::123456789012:role/opensearch-proxy-role"

ingress:
  enabled: true
  className: "nginx"
  hosts:
    - host: opensearch-proxy.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: opensearch-proxy-tls
      hosts:
        - opensearch-proxy.example.com

autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 10
EOF

# Install
helm install aws-opensearch-proxy ./charts/aws-opensearch-proxy -f my-values.yaml
```

## Configuration

### Required Values

| Parameter | Description | Example |
|-----------|-------------|---------|
| `opensearch.url` | OpenSearch endpoint URL | `https://search-domain.us-east-1.es.amazonaws.com` |

### Common Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `opensearch.region` | AWS region | `us-east-1` |
| `aws.assumeRoleArn` | Role ARN to assume | `""` |
| `replicaCount` | Number of replicas | `2` |
| `image.repository` | Docker image repository | `jpellet/aws-opensearch-proxy` |
| `image.tag` | Docker image tag | Chart appVersion |

### Service Account (IRSA)

For IAM Roles for Service Accounts (IRSA):

```yaml
serviceAccount:
  create: true
  annotations:
    eks.amazonaws.com/role-arn: "arn:aws:iam::123456789012:role/opensearch-proxy-role"
```

### Ingress

```yaml
ingress:
  enabled: true
  className: "nginx"
  annotations:
    cert-manager.io/cluster-issuer: "letsencrypt-prod"
    nginx.ingress.kubernetes.io/proxy-body-size: "0"
  hosts:
    - host: opensearch-proxy.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: opensearch-proxy-tls
      hosts:
        - opensearch-proxy.example.com
```

### Autoscaling

```yaml
autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilizationPercentage: 80
  targetMemoryUtilizationPercentage: 80
```

### Resources

```yaml
resources:
  limits:
    cpu: 500m
    memory: 512Mi
  requests:
    cpu: 100m
    memory: 128Mi
```

## Usage Examples

### Basic Installation

```bash
helm install my-proxy ./charts/aws-opensearch-proxy \
  --set opensearch.url=https://vpc-my-domain.us-east-1.es.amazonaws.com
```

### With Cross-Account Role Assumption

```bash
helm install my-proxy ./charts/aws-opensearch-proxy \
  --set opensearch.url=https://vpc-my-domain.us-east-1.es.amazonaws.com \
  --set aws.assumeRoleArn=arn:aws:iam::TARGET_ACCOUNT:role/opensearch-role
```

### With Network Policy

```yaml
networkPolicy:
  enabled: true
  ingress:
    - from:
      - namespaceSelector:
          matchLabels:
            name: my-app
```

### Testing the Deployment

```bash
# Port forward to test
kubectl port-forward svc/aws-opensearch-proxy 8080:80

# Health check
curl http://localhost:8080/_health

# OpenSearch cluster info
curl http://localhost:8080/

# List indices
curl http://localhost:8080/_cat/indices
```

## Upgrading

```bash
helm upgrade aws-opensearch-proxy ./charts/aws-opensearch-proxy \
  -f my-values.yaml
```

## Uninstalling

```bash
helm uninstall aws-opensearch-proxy
```

## Values File Reference

See [values.yaml](values.yaml) for all available configuration options.

## Troubleshooting

### Check pod logs

```bash
kubectl logs -l app.kubernetes.io/name=aws-opensearch-proxy -f
```

### Verify service account

```bash
kubectl describe serviceaccount aws-opensearch-proxy
```

### Test from within cluster

```bash
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl http://aws-opensearch-proxy/_health
```

### Common Issues

1. **403 Forbidden from OpenSearch**
   - Check IAM role has necessary permissions
   - Verify OpenSearch domain access policy
   - Check if VPC endpoint requires specific security group

2. **Cannot assume role**
   - Verify IRSA is configured correctly
   - Check trust relationship on target role
   - Ensure base role has sts:AssumeRole permission

3. **Pod fails to start**
   - Check required value `opensearch.url` is set
   - Verify image pull policy and credentials
   - Review pod events: `kubectl describe pod <pod-name>`

## Security

- Pods run as non-root user (uid 1000)
- Read-only root filesystem
- Security capabilities dropped
- Pod Disruption Budget enabled by default
- Network policies can be enabled

## Support

- GitHub: https://github.com/jpellet/aws-opensearch-proxy
- Issues: https://github.com/jpellet/aws-opensearch-proxy/issues

## License

MIT
