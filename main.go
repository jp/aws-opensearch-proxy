package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

const (
	serviceName           = "es"             // Amazon OpenSearch service name for signing
	credentialRefreshTime = 50 * time.Minute // Refresh before 60min expiry
)

var (
	// Version information set at build time via ldflags
	Version   = "dev"
	BuildDate = "unknown"
	GitCommit = "unknown"
)

type ProxyConfig struct {
	ListenPort      string
	OpenSearchURL   string
	Region          string
	AssumeRoleARN   string
	InsecureSkipTLS bool
}

type Proxy struct {
	config      *ProxyConfig
	awsConfig   aws.Config
	credentials aws.CredentialsProvider
	signer      *v4.Signer
	client      *http.Client
	credMutex   sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
}

func main() {
	var showVersion bool
	config := &ProxyConfig{}

	flag.BoolVar(&showVersion, "version", false, "Show version information and exit")
	flag.StringVar(&config.ListenPort, "port", getEnv("PORT", "8080"), "Port to listen on")
	flag.StringVar(&config.OpenSearchURL, "opensearch-url", getEnv("OPENSEARCH_URL", ""), "Amazon OpenSearch endpoint URL")
	flag.StringVar(&config.Region, "region", getEnv("AWS_REGION", "us-east-1"), "AWS Region")
	flag.StringVar(&config.AssumeRoleARN, "assume-role", getEnv("AWS_ASSUME_ROLE_ARN", ""), "AWS Role ARN to assume (optional)")
	flag.BoolVar(&config.InsecureSkipTLS, "insecure-skip-tls", getEnvBool("INSECURE_SKIP_TLS", false), "Skip TLS verification (not recommended for production)")
	flag.Parse()

	if showVersion {
		fmt.Printf("AWS OpenSearch Proxy\n")
		fmt.Printf("  Version:    %s\n", Version)
		fmt.Printf("  Build Date: %s\n", BuildDate)
		fmt.Printf("  Git Commit: %s\n", GitCommit)
		os.Exit(0)
	}

	if config.OpenSearchURL == "" {
		log.Fatal("OpenSearch URL is required (use -opensearch-url or OPENSEARCH_URL env var)")
	}

	// Validate OpenSearch URL
	if _, err := url.Parse(config.OpenSearchURL); err != nil {
		log.Fatalf("Invalid OpenSearch URL: %v", err)
	}

	proxy, err := NewProxy(config)
	if err != nil {
		log.Fatalf("Failed to initialize proxy: %v", err)
	}

	// Setup graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	server := &http.Server{
		Addr:    ":" + config.ListenPort,
		Handler: proxy,
	}

	go func() {
		log.Printf("AWS OpenSearch Proxy %s (commit: %s, built: %s)", Version, GitCommit, BuildDate)
		log.Printf("Starting server on port %s", config.ListenPort)
		log.Printf("Proxying to: %s", config.OpenSearchURL)
		if config.AssumeRoleARN != "" {
			log.Printf("Using assumed role: %s", config.AssumeRoleARN)
		}
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-stop
	log.Println("Shutting down gracefully...")
	proxy.Shutdown()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}
	log.Println("Server stopped")
}

func NewProxy(cfg *ProxyConfig) (*Proxy, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Load default AWS config
	awsConfig, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.Region))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	proxy := &Proxy{
		config:    cfg,
		awsConfig: awsConfig,
		signer:    v4.NewSigner(),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Setup HTTP client
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}

	if cfg.InsecureSkipTLS {
		log.Println("WARNING: TLS verification disabled")
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	proxy.client = &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	// Setup credentials (with or without role assumption)
	if err := proxy.setupCredentials(); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to setup credentials: %w", err)
	}

	// Start credential refresh goroutine if using assumed role
	if cfg.AssumeRoleARN != "" {
		go proxy.credentialRefreshLoop()
	}

	return proxy, nil
}

func (p *Proxy) setupCredentials() error {
	p.credMutex.Lock()
	defer p.credMutex.Unlock()

	if p.config.AssumeRoleARN == "" {
		// Use default credentials
		p.credentials = p.awsConfig.Credentials
		log.Println("Using default AWS credentials")
		return nil
	}

	// Assume role
	stsClient := sts.NewFromConfig(p.awsConfig)
	roleProvider := stscreds.NewAssumeRoleProvider(stsClient, p.config.AssumeRoleARN, func(o *stscreds.AssumeRoleOptions) {
		o.RoleSessionName = "aws-opensearch-proxy"
		o.Duration = time.Hour // Request 1 hour duration
	})

	p.credentials = aws.NewCredentialsCache(roleProvider)
	log.Printf("Assuming role: %s", p.config.AssumeRoleARN)

	// Test credentials
	_, err := p.credentials.Retrieve(p.ctx)
	if err != nil {
		return fmt.Errorf("failed to assume role: %w", err)
	}

	log.Println("Successfully assumed role")
	return nil
}

func (p *Proxy) credentialRefreshLoop() {
	ticker := time.NewTicker(credentialRefreshTime)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			log.Println("Stopping credential refresh loop")
			return
		case <-ticker.C:
			log.Println("Refreshing assumed role credentials...")
			if err := p.setupCredentials(); err != nil {
				log.Printf("Error refreshing credentials: %v", err)
			} else {
				log.Println("Credentials refreshed successfully")
			}
		}
	}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Handle health check endpoint locally
	if r.URL.Path == "/_health" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
		return
	}

	// Get current credentials
	p.credMutex.RLock()
	creds := p.credentials
	p.credMutex.RUnlock()

	// Retrieve credentials
	credentials, err := creds.Retrieve(r.Context())
	if err != nil {
		log.Printf("Failed to retrieve credentials: %v", err)
		http.Error(w, "Failed to retrieve AWS credentials", http.StatusInternalServerError)
		return
	}

	// Log credential details for debugging (without exposing secrets)
	log.Printf("Using credentials - AccessKeyId: %s..., Source: %s",
		credentials.AccessKeyID[:min(10, len(credentials.AccessKeyID))],
		credentials.Source)

	// Build target URL
	targetURL := p.config.OpenSearchURL + r.URL.Path
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	// Read body if present
	var bodyBytes []byte
	if r.Body != nil {
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			log.Printf("Failed to read request body: %v", err)
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}
		r.Body.Close()
	}

	// Create new request
	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		log.Printf("Failed to create proxy request: %v", err)
		http.Error(w, "Failed to create proxy request", http.StatusInternalServerError)
		return
	}

	// Copy headers
	for key, values := range r.Header {
		// Skip Host header as it will be set by the HTTP client
		if strings.ToLower(key) == "host" {
			continue
		}
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	// Set Content-Type if not present and body exists
	if len(bodyBytes) > 0 && proxyReq.Header.Get("Content-Type") == "" {
		proxyReq.Header.Set("Content-Type", "application/json")
	}

	// Calculate payload hash
	hash := sha256.Sum256(bodyBytes)
	payloadHash := hex.EncodeToString(hash[:])

	// Sign request with AWS Signature V4
	err = p.signer.SignHTTP(r.Context(), credentials, proxyReq, payloadHash, serviceName, p.config.Region, time.Now())
	if err != nil {
		log.Printf("Failed to sign request: %v", err)
		http.Error(w, "Failed to sign request", http.StatusInternalServerError)
		return
	}

	// Execute request
	resp, err := p.client.Do(proxyReq)
	if err != nil {
		log.Printf("Failed to execute request: %v", err)
		http.Error(w, "Failed to execute request", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Log response details for debugging
	if resp.StatusCode >= 400 {
		bodyPreview, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		log.Printf("OpenSearch error response (%d): %s", resp.StatusCode, string(bodyPreview))
		// Reset body for copying
		resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(bodyPreview), resp.Body))
	}

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Write status code
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("Failed to copy response body: %v", err)
	}

	// Log request
	log.Printf("%s %s %d", r.Method, r.URL.Path, resp.StatusCode)
}

func (p *Proxy) Shutdown() {
	p.cancel()
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true" || value == "1" || value == "yes"
	}
	return defaultValue
}
