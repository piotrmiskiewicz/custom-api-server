# Building and Pushing the Docker Image

## Prerequisites

- Docker installed and running
- Authenticated to Google Artifact Registry:
  ```bash
  gcloud auth configure-docker europe-docker.pkg.dev
  ```

## Build

```bash
IMAGE=europe-docker.pkg.dev/kyma-project/dev/custom-api-server

docker build -t ${IMAGE}:latest .
```

To tag a specific version:

```bash
VERSION=0.1.0
docker build -t ${IMAGE}:${VERSION} -t ${IMAGE}:latest .
```

## Push

```bash
docker push ${IMAGE}:latest

# Or push a specific version:
docker push ${IMAGE}:${VERSION}
docker push ${IMAGE}:latest
```

## Multi-platform build (amd64 + arm64)

If deploying to a cluster with different architecture than your build machine:

```bash
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --push \
  -t ${IMAGE}:${VERSION} \
  -t ${IMAGE}:latest \
  .
```

## Run locally

```bash
docker run --rm -p 8080:8080 ${IMAGE}:latest
```
