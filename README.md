# Well-known

A tiny service collecting and aggregating [well-known](https://www.rfc-editor.org/rfc/rfc5785) data from Services and
Ingresses in the same Kubernetes namespace. The data is merged and exposed as a JSON object.

## Installation

See the [Helm chart documentation](./charts/well-known/README.md).

## Usage

Add an annotation to a Service or Ingress:

| annotation                     | path                  |
| ------------------------------ | --------------------- |
| ` well-known.stenic.io/[path]` | `/.well-known/[path]` |

## Example

Annotations with the same path are merged across all Services and Ingresses in the namespace.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: auth-service
  annotations:
    well-known.stenic.io/openid-configuration: |
      {"issuer": "https://auth.example.com", "authorization_endpoint": "https://auth.example.com/authorize"}
```

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: api-ingress
  annotations:
    well-known.stenic.io/openid-configuration: |
      {"token_endpoint": "https://auth.example.com/token"}
```

The resulting endpoint contains the merged data from both resources:

```
curl http://[ingress]/.well-known/openid-configuration

{
    "issuer": "https://auth.example.com",
    "authorization_endpoint": "https://auth.example.com/authorize",
    "token_endpoint": "https://auth.example.com/token"
}
```
