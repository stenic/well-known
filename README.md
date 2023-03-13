# Well-known

A tiny service collecting and aggregating [well-known](https://www.rfc-editor.org/rfc/rfc5785) data from
services in the same namespace. The data is merged and exposed as a JSON object.

## Usage

Add an annotation to a service:

| annotation                     | path                  |
| ------------------------------ | --------------------- |
| ` well-known.stenic.io/[path]` | `/.well-known/[path]` |

## Example

```yaml
apiVersion: v1
kind: Service
metadata:
  annotations:
    well-known.stenic.io/test-config: |
      {"example": "value"}
```

```
curl http://[ingress]/.well-known/test-config

{
    "example": "value"
}
```
