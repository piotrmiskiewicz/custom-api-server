# Pushing the Docker Image to GitHub Container Registry

## Prerequisites

- Docker installed and running
- A GitHub account with access to the repository
- A GitHub Personal Access Token (PAT) with `write:packages` scope:
  1. Go to https://github.com/settings/tokens
  2. Generate new token (classic)
  3. Select `write:packages` (includes `read:packages`)

## Authenticate

```bash
echo $GITHUB_TOKEN | docker login ghcr.io -u <your-github-username> --password-stdin
```

## Build and Push

```bash
IMAGE=ghcr.io/piotrmiskiewicz/custom-api-server

# Build
docker build -t ${IMAGE}:latest .

# Tag a specific version
VERSION=0.1.0
docker tag ${IMAGE}:latest ${IMAGE}:${VERSION}

# Push
docker push ${IMAGE}:${VERSION}
docker push ${IMAGE}:latest
```

## Multi-platform build (amd64 + arm64)

```bash
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --push \
  -t ${IMAGE}:${VERSION} \
  -t ${IMAGE}:latest \
  .
```

## Make the package public (optional)

By default the package is private. To make it public:

1. Go to https://github.com/piotrmiskiewicz?tab=packages
2. Click on `custom-api-server`
3. Package settings → Change visibility → Public

## Use in the cluster

Update `config/deployment.yaml` to use the GitHub registry image:

```yaml
image: ghcr.io/piotrmiskiewicz/custom-api-server:latest
```

If the package is **private**, create an image pull secret:

```bash
kubectl create secret docker-registry ghcr-secret \
  --docker-server=ghcr.io \
  --docker-username=<your-github-username> \
  --docker-password=${GITHUB_TOKEN} \
  --namespace=default
```

Then reference it in `config/deployment.yaml`:

```yaml
spec:
  template:
    spec:
      imagePullSecrets:
        - name: ghcr-secret
```
