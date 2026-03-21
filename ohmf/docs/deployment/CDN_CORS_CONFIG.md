# CDN CORS Configuration for Mini-App Platform

## Overview

This document describes how to configure CORS on Content Delivery Networks (CDNs) serving mini-app assets to ensure mini-apps can load resources while respecting origin policies.

## Why CDN CORS Matters

Mini-app bundles, manifests, and assets are typically served from a CDN. When the mini-app iframe requests these resources from a different origin, the browser enforces CORS.

Example scenario:
- Mini-app runtime: `https://app.example.com`
- CDN assets: `https://cdn.example.com/miniapps/counter/index.js`
- Browser blocks request without proper CORS headers

## CloudFront (AWS)

### Configuration

1. **Create Origin Access Control (OAC)**

```
Go to CloudFront > Distributions > Select distribution > Origins
- Create OAC for S3 bucket
- Sign requests to origin: Yes
```

2. **Configure Behaviors for CORS**

Navigate to `Distribution > Behaviors`:

```
Path Pattern: /miniapps/*
Allowed HTTP Methods: GET, HEAD, OPTIONS
Cache Policy: Managed-CachingOptimized
Origin Request Policy: CORS-S3Origin
Response Headers Policy: Create custom or use managed policy
```

3. **Create Custom Response Headers Policy**

Go to `Policies > Response headers policies`:

```json
{
  "Name": "MiniappCORSPolicy",
  "Headers": {
    "Access-Control-Allow-Origin": "*",
    "Access-Control-Allow-Methods": "GET, HEAD, OPTIONS",
    "Access-Control-Allow-Headers": "Authorization, Content-Type",
    "Access-Control-Max-Age": "3600",
    "Access-Control-Expose-Headers": "ETag, X-Request-ID"
  }
}
```

Or more secure for production:

```json
{
  "Name": "MiniappCORSPolicyProduction",
  "Headers": {
    "Access-Control-Allow-Origin": "https://app.example.com",
    "Access-Control-Allow-Methods": "GET, HEAD, OPTIONS",
    "Access-Control-Allow-Headers": "Authorization, Content-Type",
    "Access-Control-Max-Age": "86400",
    "Access-Control-Expose-Headers": "ETag, Content-Length"
  }
}
```

### Verification

```bash
# Test preflight request
curl -H "Origin: https://app.example.com" \
     -H "Access-Control-Request-Method: GET" \
     -H "Access-Control-Request-Headers: authorization" \
     -X OPTIONS \
     https://cdn.example.com/miniapps/counter/manifest.json \
     -v

# Expected response headers:
# Access-Control-Allow-Origin: https://app.example.com
# Access-Control-Allow-Methods: GET, HEAD, OPTIONS
# Access-Control-Allow-Headers: Authorization, Content-Type
```

## Akamai CDN

### Configuration

1. **Add Property**

```
Property Manager > Add Property
- Origin: s3-bucket.s3.amazonaws.com
- Rule: Miniapps traffic pattern
```

2. **Configure CORS Behavior**

```
Rule: Origin = /miniapps
  - Add behavior: Cache Key
    - Include custom headers: Origin
  - Add behavior: Modified HTTP Headers
    - Response Header: Access-Control-Allow-Origin
    - Value: https://app.example.com (or * for development)
```

### Akamai Script

```
import { Crypto, Encoding, URLSearchParams } from 'cookies'

if (request.getHeader('origin')) {
  const origin = request.getHeader('origin');
  if (origin === 'https://app.example.com' || origin.match(/https:\/\/localhost:\d+/)) {
    response.setHeader('Access-Control-Allow-Origin', origin);
    response.setHeader('Access-Control-Allow-Methods', 'GET, HEAD, OPTIONS');
    response.setHeader('Access-Control-Allow-Headers', 'Authorization, Content-Type');
    response.setHeader('Access-Control-Max-Age', '3600');
  }
}
```

## Cloudflare

### Configuration via Dashboard

1. **Add Page Rule**

Go to `Page Rules > Create Page Rule`:

```
URL: https://cdn.example.com/miniapps/*
Actions:
  - Security Level: Medium
  - Browser Integrity Check: On
```

2. **Configure CORS in Worker (Recommended)**

Create `wrangler.toml`:

```toml
name = "miniapp-cors-worker"
main = "src/index.ts"
compatibility_date = "2026-03-21"

[env.production]
routes = [
  { pattern = "cdn.example.com/miniapps/*", zone_id = "YOUR_ZONE_ID" }
]
```

Create `src/index.ts`:

```typescript
const ALLOWED_ORIGINS = [
  'https://app.example.com',
  'https://miniapps.example.com',
];

export default {
  async fetch(request: Request): Promise<Response> {
    const origin = request.headers.get('Origin');

    // Handle OPTIONS preflight
    if (request.method === 'OPTIONS') {
      if (!origin || !ALLOWED_ORIGINS.includes(origin)) {
        return new Response(null, { status: 403 });
      }

      return new Response(null, {
        status: 204,
        headers: {
          'Access-Control-Allow-Origin': origin,
          'Access-Control-Allow-Methods': 'GET, HEAD, OPTIONS',
          'Access-Control-Allow-Headers': 'Authorization, Content-Type',
          'Access-Control-Max-Age': '3600',
        },
      });
    }

    // Forward actual request
    const response = await fetch(request);

    if (origin && ALLOWED_ORIGINS.includes(origin)) {
      const newHeaders = new Headers(response.headers);
      newHeaders.set('Access-Control-Allow-Origin', origin);
      newHeaders.set('Access-Control-Allow-Methods', 'GET, HEAD, OPTIONS');
      newHeaders.set('Access-Control-Allow-Headers', 'Authorization, Content-Type');
      newHeaders.set('Access-Control-Expose-Headers', 'ETag, Content-Length');

      return new Response(response.body, {
        status: response.status,
        statusText: response.statusText,
        headers: newHeaders,
      });
    }

    return response;
  },
};
```

Deploy:

```bash
wrangler deploy
```

## nginx (Self-Hosted CDN)

### Configuration

Add to `nginx.conf`:

```nginx
# CORS configuration for miniapp CDN
map $http_origin $cors_origin {
  "~^https?://localhost" $http_origin;
  "https://app.example.com" "https://app.example.com";
  "https://miniapps.example.com" "https://miniapps.example.com";
  default "";
}

server {
  listen 443 ssl http2;
  server_name cdn.example.com;

  location /miniapps/ {
    root /var/www/cdn;

    # Handle preflight
    if ($request_method = 'OPTIONS') {
      add_header 'Access-Control-Allow-Origin' $cors_origin always;
      add_header 'Access-Control-Allow-Methods' 'GET, HEAD, OPTIONS' always;
      add_header 'Access-Control-Allow-Headers' 'Authorization, Content-Type' always;
      add_header 'Access-Control-Max-Age' '3600' always;
      return 204;
    }

    # Handle actual requests
    add_header 'Access-Control-Allow-Origin' $cors_origin always;
    add_header 'Access-Control-Allow-Methods' 'GET, HEAD, OPTIONS' always;
    add_header 'Access-Control-Expose-Headers' 'ETag, Content-Length' always;
  }
}
```

Reload nginx:

```bash
sudo nginx -t
sudo systemctl reload nginx
```

## Apache (Self-Hosted CDN)

### Configuration

Add to `httpd.conf` or `.htaccess`:

```apache
<IfModule mod_headers.c>
  # Enable CORS for /miniapps/ location
  <Directory "/var/www/html/miniapps">
    Header always set Access-Control-Allow-Methods "GET, HEAD, OPTIONS"
    Header always set Access-Control-Allow-Headers "Authorization, Content-Type"
    Header always set Access-Control-Max-Age "3600"

    # Dynamic origin check (requires rewrite module)
    SetEnvIf Origin "^https?://(localhost|app\.example\.com|miniapps\.example\.com)$" CORS_ALLOWED=$0

    Header always set Access-Control-Allow-Origin "%{CORS_ALLOWED}e" env=CORS_ALLOWED

    # Preflight handling
    RewriteEngine On
    RewriteCond %{REQUEST_METHOD} ^OPTIONS$
    RewriteRule ^(.*)$ $1 [L]
  </Directory>
</IfModule>
```

## Testing CORS Configuration

### 1. Test Preflight Request

```bash
curl -i -X OPTIONS \
  -H "Origin: https://app.example.com" \
  -H "Access-Control-Request-Method: GET" \
  -H "Access-Control-Request-Headers: authorization" \
  https://cdn.example.com/miniapps/counter/manifest.json
```

Expected response:
```
HTTP/2 204 No Content
Access-Control-Allow-Origin: https://app.example.com
Access-Control-Allow-Methods: GET, HEAD, OPTIONS
Access-Control-Allow-Headers: Authorization, Content-Type
Access-Control-Max-Age: 3600
```

### 2. Test Actual Request

```bash
curl -i \
  -H "Origin: https://app.example.com" \
  https://cdn.example.com/miniapps/counter/manifest.json
```

Expected response:
```
HTTP/2 200 OK
Access-Control-Allow-Origin: https://app.example.com
Content-Type: application/json
ETag: "abc123"
```

### 3. Browser DevTools Test

In mini-app runtime console:

```javascript
fetch('https://cdn.example.com/miniapps/counter/manifest.json', {
  headers: {
    'Authorization': 'Bearer token_xyz'
  }
})
.then(r => r.json())
.then(data => console.log('Success:', data))
.catch(e => console.error('CORS Error:', e));
```

Should NOT show CORS errors in Network tab.

## Best Practices

1. **Origin Allowlist**: Always use specific origins, never `*` in production
2. **Methods**: Restrict to `GET, HEAD, OPTIONS` for asset delivery
3. **Headers**: Only expose `Authorization, Content-Type` if needed
4. **MaxAge**: Set reasonable cache time (1-24 hours)
5. **Monitoring**: Log CORS rejections and preflight failures
6. **Cache**: Include Origin in cache key for CDNs
7. **Security**: Combine with CSP headers for defense-in-depth

## Troubleshooting

### Issue: CORS error despite configuration

**Check**:
- Origin header matches allowlist exactly
- Headers served over HTTPS (some CDNs require HTTPS)
- Cache may be serving old headers (clear CDN cache)

```bash
# Clear CloudFront cache
aws cloudfront create-invalidation --distribution-id <ID> --paths "/*"

# Clear Cloudflare cache
curl -X POST "https://api.cloudflare.com/client/v4/zones/<ZONE_ID>/purge_cache" \
  -H "Authorization: Bearer <TOKEN>" \
  -H "Content-Type: application/json" \
  --data '{"files":["https://cdn.example.com/miniapps/*"]}'
```

### Issue: Preflight OPTIONS returns 403

**Check**:
- CDN allows OPTIONS method
- Origin header not empty
- Configuration correctly sets response status to 204 (not 403)

### Issue: Token not sent to CDN

**Check**:
- Mini-app runtime includes `Authorization` header
- CORS policy includes `Authorization` in allowed headers
- Preflight response includes `Access-Control-Allow-Headers: Authorization`

## Monitoring & Logging

### CloudFront Logs

```bash
# Query CloudFront logs for CORS-related requests
aws athena start-query-execution \
  --query-string "SELECT * FROM cloudfront_logs WHERE cs_host LIKE '%cdn.example.com%' AND cs_method='OPTIONS'" \
  --query-execution-context Database=cloudfront \
  --result-configuration OutputLocation=s3://query-results/
```

### Cloudflare Analytics

- Dashboard > Analytics > Web Traffic
- Filter by: Path = `/miniapps/*`, Country, Status Code
- Monitor 403, 204, 200 responses

### nginx Monitoring

```bash
# Log CORS rejections
tail -f /var/log/nginx/access.log | grep "OPTIONS.*204\|403"
```

## References

- [AWS CloudFront CORS](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/header-caching.html)
- [Cloudflare Workers CORS](https://developers.cloudflare.com/workers/examples/cors-header-proxy/)
- [MDN CORS](https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS)
