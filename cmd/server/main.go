package main

import (
    "flag"
    "log"
    "os"
    "os/signal"
    "syscall"

    "github.com/piotrmiskiewicz/custom-api-server/pkg/apiserver"
)

func main() {
    certFile := flag.String("tls-cert-file", "", "Path to TLS certificate. If empty, a self-signed certificate is generated.")
    keyFile := flag.String("tls-key-file", "", "Path to TLS key. If empty, a self-signed key is generated.")
    addr := flag.String("bind-address", ":8443", "Address to listen on.")
    flag.Parse()

    srv, err := apiserver.New(*certFile, *keyFile, *addr)
    if err != nil {
        log.Fatalf("failed to create server: %v", err)
    }

    stopCh := make(chan struct{})
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
    go func() {
        <-sigCh
        close(stopCh)
    }()

    log.Println("Starting custom-api-server on :8443")

    if err := srv.PrepareRun().Run(stopCh); err != nil {
        log.Fatalf("server error: %v", err)
    }
}
