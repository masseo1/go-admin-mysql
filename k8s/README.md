# Kubernetes Deployment Guide

These manifests deploy go-admin-mysql to a single namespace with a persistent volume for server data.

---

## Prerequisites

- `kubectl` configured for your cluster
- A container registry with the built image
- An ingress controller (e.g., nginx-ingress)
- (Optional) cert-manager for automatic TLS certificates

---

## Step-by-step

### 1. Create the secret

**Do not commit real credentials.** Create the secret directly from the command line:

```bash
kubectl create namespace mysql-admin

kubectl create secret generic mysql-admin-secret \
  --namespace mysql-admin \
  --from-literal=ADMIN_USER=admin \
  --from-literal=ADMIN_PASS="$(openssl rand -base64 32)" \
  --from-literal=ENCRYPTION_KEY="$(openssl rand -hex 32)"
```

> Save the generated values somewhere secure (password manager, Vault, etc.). You will need `ENCRYPTION_KEY` if you ever need to re-create the secret while keeping existing server data.

The `k8s/secret.yaml` in this repo contains only placeholder values and is provided as a reference — it is safe to commit.

---

### 2. Push your image

```bash
make REGISTRY=your-registry.example.com TAG=v1.0.0 docker-build docker-push
```

### 3. Update the deployment image

Edit `k8s/deployment.yaml` and replace the image placeholder:

```yaml
image: your-registry.example.com/go-admin-mysql:v1.0.0
```

### 4. Configure the ingress

Edit `k8s/ingress.yaml`:

| Field | What to set |
|---|---|
| `host` | Your internal hostname (e.g., `mysql-admin.internal.example.com`) |
| `secretName` | TLS certificate secret (e.g., from cert-manager) |
| `whitelist-source-range` | Your VPN or internal CIDR range |
| `ingressClassName` | Your cluster's ingress class |

### 5. Configure the storage class

Edit `k8s/pvc.yaml` — change `storageClassName` to match your cluster:

| Platform | Storage class |
|---|---|
| GKE | `standard` |
| AKS | `managed-premium` |
| EKS + EFS | `efs-sc` |
| EKS + EBS | `gp2` or `gp3` |

### 6. Update liveness/readiness probe credentials

The probes use HTTP Basic Auth. Generate the correct base64 header value:

```bash
echo -n "admin:your-actual-admin-pass" | base64
```

Replace both occurrences of the `Authorization` header value in `k8s/deployment.yaml` with the output.

### 7. Deploy

```bash
make k8s-deploy
```

Or step by step:

```bash
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/pvc.yaml
kubectl apply -f k8s/secret.yaml
kubectl apply -f k8s/deployment.yaml
kubectl apply -f k8s/service.yaml
kubectl apply -f k8s/ingress.yaml
```

### 8. Verify

```bash
kubectl get pods -n mysql-admin
kubectl logs -n mysql-admin deployment/mysql-admin
```

---

## Tear down

```bash
make k8s-delete
# equivalent to: kubectl delete namespace mysql-admin
```

---

## Notes

- The deployment runs as UID 1001 with a read-only root filesystem. Only the `/data` volume mount is writable.
- Only one replica is supported — `servers.json` is written to a single PVC. Running multiple replicas will cause data races.
- The ingress restricts access to RFC-1918 ranges by default. Adjust `whitelist-source-range` to match your network.
