// SPDX-License-Identifier: MIT
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/StakoCP/stako/internal/agentclient"
	"github.com/StakoCP/stako/internal/api"
	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/jobs"
	"github.com/StakoCP/stako/internal/store"
	"github.com/StakoCP/stako/internal/version"
	webassets "github.com/StakoCP/stako/web"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		runServe(os.Args[2:])
	case "bootstrap-admin":
		runBootstrapAdmin(os.Args[2:])
	case "version":
		fmt.Printf("stako-core %s (%s)\n", version.Version, version.Commit)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: stako-core <serve|bootstrap-admin|version> [flags]")
}

func defaultDataDir() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(".", "data")
	}
	return "/var/lib/stako"
}

func defaultKeyPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(".", "data", "master.key")
	}
	return "/etc/stako/keys/master.key"
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	listen := fs.String("listen", ":8083", "HTTP/HTTPS listen address")
	dbPath := fs.String("db", filepath.Join(defaultDataDir(), "stako.db"), "SQLite database path")
	keyPath := fs.String("master-key", defaultKeyPath(), "Master encryption key path")
	agentSock := fs.String("agent-sock", "", "stako-agent Unix socket path")
	dryRunAgent := fs.Bool("dry-run-agent", runtime.GOOS == "windows", "Simulate agent calls without socket")
	insecureHTTP := fs.Bool("insecure-http", false, "Serve plain HTTP (development only)")
	tlsCert := fs.String("tls-cert", "", "TLS certificate path")
	tlsKey := fs.String("tls-key", "", "TLS private key path")
	_ = fs.Parse(args)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	st, err := store.Open(*dbPath)
	if err != nil {
		logger.Error("open store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	enc, err := crypto.LoadOrCreateMasterKey(*keyPath)
	if err != nil {
		logger.Error("load master key", "err", err)
		os.Exit(1)
	}

	sock := *agentSock
	if sock == "" && !*dryRunAgent && runtime.GOOS != "windows" {
		sock = "/run/stako/agent.sock"
	}
	ac := agentclient.New(sock, *dryRunAgent)
	jq := jobs.NewQueue(st, ac)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	jq.Start(ctx)

	srv := api.NewServer(api.Config{
		Store:        st,
		Agent:        ac,
		Jobs:         jq,
		Encryptor:    enc,
		Logger:       logger,
		WebFS:        webassets.FS(),
		InsecureHTTP: *insecureHTTP,
	})

	httpSrv := &http.Server{
		Addr:              *listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	if !*insecureHTTP {
		certFile, keyFile := *tlsCert, *tlsKey
		if certFile == "" || keyFile == "" {
			certFile = filepath.Join(filepath.Dir(*dbPath), "panel-selfsigned.crt")
			keyFile = filepath.Join(filepath.Dir(*dbPath), "panel-selfsigned.key")
			if err := ensureSelfSignedCert(certFile, keyFile); err != nil {
				logger.Error("generate self-signed cert", "err", err)
				os.Exit(1)
			}
			logger.Info("using self-signed TLS certificate", "cert", certFile)
		}
		httpSrv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		go func() {
			logger.Info("stako-core listening (HTTPS)", "addr", *listen)
			if err := httpSrv.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
				logger.Error("listen tls", "err", err)
				os.Exit(1)
			}
		}()
	} else {
		go func() {
			logger.Warn("stako-core listening (INSECURE HTTP dev mode)", "addr", *listen)
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("listen http", "err", err)
				os.Exit(1)
			}
		}()
	}

	<-ctx.Done()
	logger.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutCtx)
	jq.Stop()
}

func runBootstrapAdmin(args []string) {
	fs := flag.NewFlagSet("bootstrap-admin", flag.ExitOnError)
	dbPath := fs.String("db", filepath.Join(defaultDataDir(), "stako.db"), "SQLite database path")
	username := fs.String("username", "admin", "Admin username")
	email := fs.String("email", "admin@localhost", "Admin email")
	password := fs.String("password", "", "Admin password (auto-generated if empty)")
	_ = fs.Parse(args)

	st, err := store.Open(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open db: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	pw := *password
	generated := false
	if pw == "" {
		h, err := crypto.RandomHex(12)
		if err != nil {
			fmt.Fprintf(os.Stderr, "generate password: %v\n", err)
			os.Exit(1)
		}
		pw = h
		generated = true
	} else if err := crypto.ValidatePasswordStrength(pw); err != nil {
		fmt.Fprintf(os.Stderr, "invalid password: %v\n", err)
		os.Exit(1)
	}

	hash, err := crypto.HashPassword(pw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hash password: %v\n", err)
		os.Exit(1)
	}
	id, _ := crypto.RandomID("u")
	u := &store.User{
		ID:           id,
		Username:     *username,
		Email:        *email,
		PasswordHash: hash,
		Role:         "admin",
		Status:       "active",
		Shell:        "nologin",
	}
	if err := st.CreateUser(context.Background(), u); err != nil {
		fmt.Fprintf(os.Stderr, "create admin: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Admin user created: %s (%s)\n", u.Username, u.ID)
	if generated {
		fmt.Printf("Generated password (save now): %s\n", pw)
	}
}

func ensureSelfSignedCert(certPath, keyPath string) error {
	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(certPath), 0o750); err != nil {
		return err
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Stako Panel"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return err
	}
	certOut, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer certOut.Close()
	_ = pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return err
	}
	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	return pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
}
