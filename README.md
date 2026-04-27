# custom-api-server
An experiment with custom API server for k8s

## Standalone server with Postgres backend

1. Start docker compose:

```bash
docker compose up --build
```

2. The API is available at `https://localhost:8444`.
Example request:

```bash
kubectl --insecure-skip-tls-verify=true \
  --server=https://localhost:8444 \
  --namespace=default \
  get solutions
```

3. Create 50k solutions in 10 namespaces

```bash
python script/create_solutions.py
```

4. Get solutions within a namespace:

```bash
kubectl --server=https://localhost:8444 --insecure-skip-tls-verify get solutions -n ns-3
```

## Play with k3d

1. Start a k3d cluster:

```bash
k3d cluster create
```

2. Deploy the API server to the cluster:

```bash
kubectl apply -f config/deployment.yaml
kubectl apply -f config/apiservice.yaml
```
