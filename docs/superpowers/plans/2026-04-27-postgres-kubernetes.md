# Postgres in Kubernetes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Postgres Deployment, Service, and Secret to `config/deployment.yaml` and configure the `custom-api-server` Deployment to connect to it.

**Architecture:** A single `Secret` holds postgres credentials shared by both the postgres pod (via `envFrom`) and the api-server pod (via `secretKeyRef`). The api-server constructs `POSTGRES_DSN` using Kubernetes variable substitution from those secret-sourced env vars. Postgres uses an `emptyDir` volume — data is ephemeral, suitable for dev.

**Tech Stack:** Kubernetes YAML (no new code, no new dependencies)

---

### Task 1: Add postgres Secret, Deployment, and Service to config/deployment.yaml

**Files:**
- Modify: `config/deployment.yaml`

- [ ] **Step 1: Append the Secret to config/deployment.yaml**

Add the following at the end of `config/deployment.yaml` (after the existing Service resource, separated by `---`):

```yaml
---
apiVersion: v1
kind: Secret
metadata:
  name: postgres-credentials
  namespace: default
type: Opaque
stringData:
  POSTGRES_USER: apiserver
  POSTGRES_PASSWORD: apiserver
  POSTGRES_DB: apiserver
```

> Note: `stringData` lets you write plain text — Kubernetes base64-encodes it automatically.

- [ ] **Step 2: Append the postgres Deployment**

```yaml
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: postgres
  namespace: default
  labels:
    app: postgres
spec:
  replicas: 1
  selector:
    matchLabels:
      app: postgres
  template:
    metadata:
      labels:
        app: postgres
    spec:
      containers:
        - name: postgres
          image: postgres:16
          envFrom:
            - secretRef:
                name: postgres-credentials
          volumeMounts:
            - name: data
              mountPath: /var/lib/postgresql/data
      volumes:
        - name: data
          emptyDir: {}
```

- [ ] **Step 3: Append the postgres Service**

```yaml
---
apiVersion: v1
kind: Service
metadata:
  name: postgres
  namespace: default
spec:
  selector:
    app: postgres
  ports:
    - port: 5432
      targetPort: 5432
```

- [ ] **Step 4: Commit**

```bash
git add config/deployment.yaml
git commit -m "feat: add postgres Secret, Deployment, and Service"
```

---

### Task 2: Wire custom-api-server Deployment to use Postgres

**Files:**
- Modify: `config/deployment.yaml`

- [ ] **Step 1: Add env vars to the custom-api-server container**

In the existing `custom-api-server` Deployment, find the `containers` section and add an `env` block to the `custom-api-server` container:

```yaml
          env:
            - name: STORAGE_BACKEND
              value: postgres
            - name: POSTGRES_USER
              valueFrom:
                secretKeyRef:
                  name: postgres-credentials
                  key: POSTGRES_USER
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: postgres-credentials
                  key: POSTGRES_PASSWORD
            - name: POSTGRES_DB
              valueFrom:
                secretKeyRef:
                  name: postgres-credentials
                  key: POSTGRES_DB
            - name: POSTGRES_DSN
              value: "postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@postgres.default.svc.cluster.local:5432/$(POSTGRES_DB)?sslmode=disable"
```

The full `containers` entry should look like:

```yaml
      containers:
        - name: custom-api-server
          image: ghcr.io/piotrmiskiewicz/custom-api-server:latest
          imagePullPolicy: Always
          ports:
            - containerPort: 8443
          env:
            - name: STORAGE_BACKEND
              value: postgres
            - name: POSTGRES_USER
              valueFrom:
                secretKeyRef:
                  name: postgres-credentials
                  key: POSTGRES_USER
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: postgres-credentials
                  key: POSTGRES_PASSWORD
            - name: POSTGRES_DB
              valueFrom:
                secretKeyRef:
                  name: postgres-credentials
                  key: POSTGRES_DB
            - name: POSTGRES_DSN
              value: "postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@postgres.default.svc.cluster.local:5432/$(POSTGRES_DB)?sslmode=disable"
          readinessProbe:
            httpGet:
              path: /healthz
              port: 8443
              scheme: HTTPS
          livenessProbe:
            httpGet:
              path: /healthz
              port: 8443
              scheme: HTTPS
```

- [ ] **Step 2: Verify the YAML is valid**

```bash
kubectl apply --dry-run=client -f config/deployment.yaml
```

Expected output (no errors):
```
secret/postgres-credentials configured (dry run)
deployment.apps/postgres configured (dry run)
service/postgres configured (dry run)
deployment.apps/custom-api-server configured (dry run)
service/custom-api-server configured (dry run)
```

- [ ] **Step 3: Commit**

```bash
git add config/deployment.yaml
git commit -m "feat: configure custom-api-server to use postgres backend"
```

---

### Task 3: Smoke-test on a local cluster

**Files:** none (verification only)

- [ ] **Step 1: Apply the manifests**

```bash
kubectl apply -f config/deployment.yaml
kubectl apply -f config/apiservice.yaml
```

- [ ] **Step 2: Wait for both pods to be ready**

```bash
kubectl rollout status deployment/postgres
kubectl rollout status deployment/custom-api-server
```

Expected: `deployment "postgres" successfully rolled out` and `deployment "custom-api-server" successfully rolled out`

- [ ] **Step 3: Verify the api-server can reach postgres**

```bash
kubectl logs deployment/custom-api-server | head -20
```

Expected: no `POSTGRES_DSN` or connection errors in the logs.

- [ ] **Step 4: Create a Solution and list it back**

```bash
kubectl apply -f example/solution.yaml
kubectl get solutions --all-namespaces
```

Expected: the solution appears in the list, confirming reads/writes go through postgres.
