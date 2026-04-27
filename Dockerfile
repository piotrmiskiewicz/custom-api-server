FROM golang:1.26 AS builder

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY pkg/ pkg/

RUN CGO_ENABLED=0 GOOS=linux go build -o custom-api-server ./cmd/server/

FROM gcr.io/distroless/static:nonroot
LABEL author=piotr.miskiewicz

COPY --from=builder /workspace/custom-api-server /custom-api-server

EXPOSE 8080

ENTRYPOINT ["/custom-api-server"]
