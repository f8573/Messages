# S3 CORS Configuration for Mini-App Assets

## Overview

This document describes how to configure AWS S3 for serving mini-app manifests, bundles, and assets with proper CORS headers. This is essential when mini-apps are hosted on S3 and accessed from different origins.

## Architecture

```
Mini-App Runtime (https://app.example.com)
    |
    | fetch manifest.json
    | Origin: https://app.example.com
    |
    v
S3 Bucket (https://miniapps-bucket.s3.amazonaws.com)
    |
    | CORS validation
    | Check Origin allowlist
    |
    v
manifest.json + CORS headers
```

## S3 CORS Configuration

### 1. Basic Configuration (Development)

Navigate to S3 bucket > Permissions > CORS:

```json
[
  {
    "AllowedOrigins": [
      "http://localhost:*",
      "http://127.0.0.1:*"
    ],
    "AllowedMethods": [
      "GET",
      "HEAD"
    ],
    "AllowedHeaders": [
      "Authorization",
      "Content-Type"
    ],
    "ExposeHeaders": [
      "ETag",
      "x-amz-version-id"
    ],
    "MaxAgeSeconds": 3600
  }
]
```

### 2. Production Configuration

```json
[
  {
    "AllowedOrigins": [
      "https://app.example.com",
      "https://miniapps.example.com"
    ],
    "AllowedMethods": [
      "GET",
      "HEAD"
    ],
    "AllowedHeaders": [
      "Authorization",
      "Content-Type",
      "x-amz-meta-*"
    ],
    "ExposeHeaders": [
      "ETag",
      "x-amz-version-id",
      "x-amz-meta-bundle-hash"
    ],
    "MaxAgeSeconds": 86400
  }
]
```

### 3. Via AWS CLI

```bash
cat > cors.json << 'EOF'
[
  {
    "AllowedOrigins": [
      "https://app.example.com",
      "https://miniapps.example.com"
    ],
    "AllowedMethods": ["GET", "HEAD"],
    "AllowedHeaders": ["Authorization", "Content-Type"],
    "ExposeHeaders": ["ETag", "x-amz-version-id"],
    "MaxAgeSeconds": 86400
  }
]
EOF

# Apply CORS configuration
aws s3api put-bucket-cors \
  --bucket miniapps-bucket \
  --cors-configuration file://cors.json \
  --region us-east-1
```

### 4. Via Terraform

```hcl
resource "aws_s3_bucket" "miniapps" {
  bucket = "miniapps-bucket"
}

resource "aws_s3_bucket_cors_configuration" "miniapps" {
  bucket = aws_s3_bucket.miniapps.id

  cors_rule {
    allowed_headers = ["Authorization", "Content-Type", "x-amz-meta-*"]
    allowed_methods = ["GET", "HEAD"]
    allowed_origins = [
      "https://app.example.com",
      "https://miniapps.example.com"
    ]
    expose_headers  = ["ETag", "x-amz-version-id", "x-amz-meta-bundle-hash"]
    max_age_seconds = 86400
  }
}
```

### 5. Via CloudFormation

```yaml
Resources:
  MiniappsBucket:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: miniapps-bucket
      CorsConfiguration:
        CorsRules:
          - AllowedHeaders:
              - Authorization
              - Content-Type
              - x-amz-meta-*
            AllowedMethods:
              - GET
              - HEAD
            AllowedOrigins:
              - https://app.example.com
              - https://miniapps.example.com
            ExposeHeaders:
              - ETag
              - x-amz-version-id
              - x-amz-meta-bundle-hash
            MaxAge: 86400
```

## S3 + CloudFront Setup

When using CloudFront in front of S3, you typically want to:

1. **S3 CORS** for direct S3 access
2. **CloudFront CORS** for CDN-cached access

### Configuration Priority

```
Direct S3 Access:
  Mini-App Runtime --CORS--> S3 Bucket CORS Config

CloudFront Access:
  Mini-App Runtime --CORS--> CloudFront Distribution CORS Headers
```

### Setup Steps

1. **Configure S3 CORS** (as above)

2. **Create CloudFront Origin Access Control (OAC)**

```bash
aws cloudfront create-origin-access-control \
  --origin-access-control-config \
  Name=S3OAC,\
  OriginAccessControlOriginType=s3,\
  SigningBehavior=always,\
  SigningProtocol=sigv4
```

3. **Update S3 Bucket Policy**

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Service": "cloudfront.amazonaws.com"
      },
      "Action": "s3:GetObject",
      "Resource": "arn:aws:s3:::miniapps-bucket/*",
      "Condition": {
        "StringEquals": {
          "AWS:SourceArn": "arn:aws:cloudfront::ACCOUNT_ID:distribution/DISTRIBUTION_ID"
        }
      }
    }
  ]
}
```

4. **Configure CloudFront Response Headers**

In CloudFront Distribution > Response headers policies:

```json
{
  "Name": "S3MiniappCORS",
  "Headers": {
    "Access-Control-Allow-Origin": "https://app.example.com",
    "Access-Control-Allow-Methods": "GET, HEAD, OPTIONS",
    "Access-Control-Allow-Headers": "Authorization, Content-Type",
    "Access-Control-Max-Age": "86400",
    "Access-Control-Expose-Headers": "ETag, Content-Length"
  }
}
```

## Testing S3 CORS Configuration

### 1. Test Direct S3 Access

```bash
# Test preflight
curl -i -X OPTIONS \
  -H "Origin: https://app.example.com" \
  -H "Access-Control-Request-Method: GET" \
  -H "Access-Control-Request-Headers: authorization" \
  https://miniapps-bucket.s3.amazonaws.com/counter/manifest.json

# Expected: 200 OK with CORS headers
```

### 2. Test via AWS CLI

```bash
# Get CORS configuration to verify
aws s3api get-bucket-cors \
  --bucket miniapps-bucket \
  --region us-east-1
```

### 3. Test from Browser

```javascript
// In mini-app runtime console
fetch('https://miniapps-bucket.s3.amazonaws.com/counter/manifest.json', {
  headers: {
    'Authorization': 'Bearer token_xyz'
  }
})
.then(r => {
  console.log('Status:', r.status);
  console.log('Headers:', Object.fromEntries(r.headers));
  return r.json();
})
.then(data => console.log('Success:', data))
.catch(e => console.error('Error:', e.message));
```

Check browser DevTools for CORS errors.

### 4. Test Presigned URLs

Presigned URLs can bypass some CORS requirements, but CORS headers are still useful:

```bash
# Generate presigned URL (15 minutes valid)
aws s3 presign \
  s3://miniapps-bucket/counter/manifest.json \
  --expires-in 900
```

## S3 Access Control & Security

### 1. Block Public Access (Recommended)

```bash
aws s3api put-public-access-block \
  --bucket miniapps-bucket \
  --public-access-block-configuration \
  BlockPublicAcls=true,\
  IgnorePublicAcls=true,\
  BlockPublicPolicy=true,\
  RestrictPublicBuckets=true
```

### 2. Use IAM Policies

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "AWS": "arn:aws:iam::ACCOUNT_ID:role/miniapp-lambda-role"
      },
      "Action": "s3:GetObject",
      "Resource": "arn:aws:s3:::miniapps-bucket/*/manifest.json"
    }
  ]
}
```

### 3. Enable Versioning

```bash
aws s3api put-bucket-versioning \
  --bucket miniapps-bucket \
  --versioning-configuration Status=Enabled
```

### 4. Enable Logging

```bash
# Create logging bucket first
aws s3 mb s3://miniapps-bucket-logs

# Enable S3 access logging
aws s3api put-bucket-logging \
  --bucket miniapps-bucket \
  --bucket-logging-status '{
    "LoggingEnabled": {
      "TargetBucket": "miniapps-bucket-logs",
      "TargetPrefix": "access-logs/"
    }
  }'
```

## S3 Object Metadata

### Setting Custom Metadata

```bash
# Upload with metadata
aws s3 cp manifest.json s3://miniapps-bucket/counter/ \
  --metadata "bundle-hash=abc123,version=1.0.0"

# Expose metadata via CORS
aws s3api put-bucket-cors \
  --bucket miniapps-bucket \
  --cors-configuration file://cors.json
```

### In Upload Script

```bash
#!/bin/bash
BUNDLE_HASH=$(sha256sum counter.bundle.js | cut -d' ' -f1)
VERSION="1.0.0"

aws s3 cp counter.bundle.js \
  s3://miniapps-bucket/counter/ \
  --metadata "bundle-hash=${BUNDLE_HASH},version=${VERSION}" \
  --content-type application/javascript \
  --cache-control "public, max-age=31536000"
```

## Troubleshooting

### Issue: 403 Forbidden on preflight OPTIONS

**Check**:
- Origin is in CORS AllowedOrigins list
- Bucket CORS configuration saved (might need wait for propagation)
- S3 URL is accessible (test with curl without CORS)

```bash
# Verify CORS is set
aws s3api get-bucket-cors --bucket miniapps-bucket

# If empty, reapply configuration
aws s3api put-bucket-cors \
  --bucket miniapps-bucket \
  --cors-configuration file://cors.json
```

### Issue: CORS headers not in response

**Check**:
- HTTP method is GET/HEAD (preflight is OPTIONS)
- Origin header matches exactly (case-sensitive)
- Not using wildcard origin with credentials

```bash
# Debug with verbose curl
curl -v \
  -H "Origin: https://app.example.com" \
  https://miniapps-bucket.s3.amazonaws.com/counter/manifest.json 2>&1 | grep -i access-control
```

### Issue: Presigned URL with CORS

Presigned URLs can have CORS issues due to signature validation. Solution:

```python
import boto3
from botocore.exceptions import ClientError

s3_client = boto3.client('s3')

# Generate URL with extra time for browser to process
url = s3_client.generate_presigned_url(
    'get_object',
    Params={
        'Bucket': 'miniapps-bucket',
        'Key': 'counter/manifest.json'
    },
    ExpiresIn=3600,
    HttpMethod='GET'
)

# URL will work cross-origin without CORS headers
print(url)
```

## Performance Optimization

### 1. Cache Invalidation

```bash
# After uploading new version
aws s3 cp manifest.json \
  s3://miniapps-bucket/counter/manifest.json \
  --cache-control "public, max-age=3600"

# If using CloudFront, invalidate
aws cloudfront create-invalidation \
  --distribution-id DISTRIBUTION_ID \
  --paths "/counter/manifest.json"
```

### 2. Object Lifecycle Policies

```bash
aws s3api put-bucket-lifecycle-configuration \
  --bucket miniapps-bucket \
  --lifecycle-configuration '{
    "Rules": [
      {
        "Id": "DeleteOldVersions",
        "Status": "Enabled",
        "NoncurrentVersionExpiration": {
          "NoncurrentDays": 30
        }
      }
    ]
  }'
```

### 3. S3 Transfer Acceleration

```bash
# Enable transfer acceleration for uploads
aws s3api put-bucket-accelerate-configuration \
  --bucket miniapps-bucket \
  --accelerate-configuration Status=Enabled

# Use accelerated endpoint
aws s3 cp manifest.json \
  s3://miniapps-bucket/counter/ \
  --region us-east-1 \
  --endpoint-url https://miniapps-bucket.s3-accelerate.amazonaws.com
```

## Monitoring & Logging

### CloudWatch Metrics

```bash
# Enable request metrics (requires S3 request metrics)
aws s3api put-bucket-metrics-configuration \
  --bucket miniapps-bucket \
  --id EntireBucket \
  --metrics-configuration '{
    "Id": "EntireBucket",
    "Filter": {"Prefix": "counter/"}
  }'
```

### S3 Access Logs Analysis

```bash
# Query logs for CORS requests
aws athena start-query-execution \
  --query-string "SELECT * FROM s3_logs WHERE request_uri LIKE '%manifest.json%'" \
  --result-configuration OutputLocation=s3://query-results/
```

## References

- [AWS S3 CORS Documentation](https://docs.aws.amazon.com/AmazonS3/latest/userguide/cors.html)
- [MDN CORS Specification](https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS)
- [AWS CloudFront with S3](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/DownloadDistS3AndCustomOrigins.html)
