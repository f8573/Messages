package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"
)

// LoadTestConfig holds load testing parameters
type LoadTestConfig struct {
	NumMessages     int
	NumRecipients   int
	EncryptionRatio float64 // percentage of messages that are encrypted
	Concurrent      int
	Verbose         bool
}

// LoadTestResult holds statistics from load test
type LoadTestResult struct {
	TotalMessages     int
	EncryptedMessages int
	PlaintextMessages int
	TotalTime         time.Duration
	MessageRate       float64 // messages per second
	AvgTime           time.Duration
	MinTime           time.Duration
	MaxTime           time.Duration
	ErrorCount        int
}

// LoadTester simulates encrypted message generation and validation
type LoadTester struct {
	config LoadTestConfig
	result LoadTestResult
	mu     sync.Mutex
	times  []time.Duration
}

// NewLoadTester creates a new load tester
func NewLoadTester(config LoadTestConfig) *LoadTester {
	return &LoadTester{
		config: config,
		times:  make([]time.Duration, 0, config.NumMessages),
	}
}

// GenerateEncryptedMessage simulates E2EE message generation
func (lt *LoadTester) GenerateEncryptedMessage() (map[string]any, error) {
	// Generate session key
	sessionKey := make([]byte, 32)
	if _, err := rand.Read(sessionKey); err != nil {
		return nil, err
	}

	// Generate nonce
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	// Generate ephemeral keypair for signing
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	// Sign the ciphertext
	ciphertext := sessionKey
	signature := ed25519.Sign(privKey, ciphertext)

	// Compute fingerprint
	fingerprintHash := make([]byte, 32)
	rand.Read(fingerprintHash)

	msg := map[string]any{
		"ciphertext": base64.StdEncoding.EncodeToString(ciphertext),
		"nonce":      base64.StdEncoding.EncodeToString(nonce),
		"encryption": map[string]any{
			"scheme":           "OHMF_SIGNAL_V1",
			"sender_user_id":   fmt.Sprintf("user-%d", rand.Intn(1000)),
			"sender_device_id": fmt.Sprintf("device-%d", rand.Intn(100)),
			"sender_signature": base64.StdEncoding.EncodeToString(signature),
			"sender_public_key": base64.StdEncoding.EncodeToString(pubKey),
			"recipients":       lt.generateRecipients(),
		},
	}

	return msg, nil
}

// GeneratePlaintextMessage simulates plaintext message generation
func (lt *LoadTester) GeneratePlaintextMessage() map[string]any {
	return map[string]any{
		"text": fmt.Sprintf("Test message %d", rand.Intn(10000)),
	}
}

// generateRecipients creates a list of recipient device keys
func (lt *LoadTester) generateRecipients() []map[string]any {
	recipients := make([]map[string]any, 0, lt.config.NumRecipients)

	for i := 0; i < lt.config.NumRecipients; i++ {
		key := make([]byte, 32)
		rand.Read(key)
		nonce := make([]byte, 12)
		rand.Read(nonce)

		recipients = append(recipients, map[string]any{
			"user_id":     fmt.Sprintf("user-%d", rand.Intn(1000)),
			"device_id":   fmt.Sprintf("device-%d", rand.Intn(100)),
			"wrapped_key": base64.StdEncoding.EncodeToString(key),
			"wrap_nonce":  base64.StdEncoding.EncodeToString(nonce),
		})
	}

	return recipients
}

// ProcessMessage simulates message validation
func (lt *LoadTester) ProcessMessage(msg map[string]any) error {
	// Simulate validation time
	if _, ok := msg["encryption"]; ok {
		// Encrypted message takes slightly longer
		time.Sleep(time.Microsecond * 10)
	} else {
		// Plaintext message
		time.Sleep(time.Microsecond * 5)
	}
	return nil
}

// Run executes the load test
func (lt *LoadTester) Run() {
	fmt.Printf("Starting load test: %d messages, %.0f%% encrypted\n", lt.config.NumMessages, lt.config.EncryptionRatio*100)

	startTime := time.Now()
	sem := make(chan struct{}, lt.config.Concurrent)
	var wg sync.WaitGroup

	for i := 0; i < lt.config.NumMessages; i++ {
		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore slot

		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore slot

			opStart := time.Now()

			// Decide if encrypted based on ratio
			ratio, _ := big.NewFloat(lt.config.EncryptionRatio).Float64()
			rnd, _ := big.NewFloat(rand.Float64()).Float64()

			var msg map[string]any
			var err error

			if rnd < ratio {
				msg, err = lt.GenerateEncryptedMessage()
				lt.mu.Lock()
				lt.result.EncryptedMessages++
				lt.mu.Unlock()
			} else {
				msg = lt.GeneratePlaintextMessage()
				lt.mu.Lock()
				lt.result.PlaintextMessages++
				lt.mu.Unlock()
			}

			if err != nil {
				lt.mu.Lock()
				lt.result.ErrorCount++
				lt.mu.Unlock()
				return
			}

			if err := lt.ProcessMessage(msg); err != nil {
				lt.mu.Lock()
				lt.result.ErrorCount++
				lt.mu.Unlock()
				return
			}

			elapsed := time.Since(opStart)
			lt.mu.Lock()
			lt.times = append(lt.times, elapsed)
			if lt.config.Verbose && idx%100 == 0 {
				fmt.Printf("  Processed %d messages...\n", idx)
			}
			lt.mu.Unlock()
		}(i)
	}

	wg.Wait()
	totalTime := time.Since(startTime)

	lt.result.TotalMessages = lt.config.NumMessages
	lt.result.TotalTime = totalTime
	lt.result.MessageRate = float64(lt.config.NumMessages) / totalTime.Seconds()

	// Calculate statistics
	lt.calculateStats()

	lt.PrintResults()
}

// calculateStats computes min, max, and average times
func (lt *LoadTester) calculateStats() {
	if len(lt.times) == 0 {
		return
	}

	minTime := lt.times[0]
	maxTime := lt.times[0]
	totalTime := time.Duration(0)

	for _, t := range lt.times {
		if t < minTime {
			minTime = t
		}
		if t > maxTime {
			maxTime = t
		}
		totalTime += t
	}

	lt.result.MinTime = minTime
	lt.result.MaxTime = maxTime
	lt.result.AvgTime = time.Duration(totalTime.Nanoseconds() / int64(len(lt.times)))
}

// PrintResults prints load test results
func (lt *LoadTester) PrintResults() {
	fmt.Println("\n" + "="*60)
	fmt.Println("LOAD TEST RESULTS")
	fmt.Println("="*60)
	fmt.Printf("Total Messages:       %d\n", lt.result.TotalMessages)
	fmt.Printf("  - Encrypted:       %d (%.1f%%)\n", lt.result.EncryptedMessages, float64(lt.result.EncryptedMessages)*100/float64(lt.result.TotalMessages))
	fmt.Printf("  - Plaintext:       %d (%.1f%%)\n", lt.result.PlaintextMessages, float64(lt.result.PlaintextMessages)*100/float64(lt.result.TotalMessages))
	fmt.Printf("Errors:              %d\n", lt.result.ErrorCount)
	fmt.Printf("Total Time:          %v\n", lt.result.TotalTime)
	fmt.Printf("Message Rate:        %.2f msg/sec\n", lt.result.MessageRate)
	fmt.Printf("Average Time:        %v\n", lt.result.AvgTime)
	fmt.Printf("Min Time:            %v\n", lt.result.MinTime)
	fmt.Printf("Max Time:            %v\n", lt.result.MaxTime)
	fmt.Println("="*60)
}

func main() {
	numMessages := flag.Int("messages", 1000, "Number of messages to process")
	numRecipients := flag.Int("recipients", 5, "Number of recipients per encrypted message")
	encryptionRatio := flag.Float64("encrypted", 0.5, "Ratio of encrypted messages (0.0-1.0)")
	concurrent := flag.Int("concurrent", 10, "Number of concurrent message processors")
	verbose := flag.Bool("verbose", false, "Verbose output")

	flag.Parse()

	if *numMessages < 0 || *concurrent < 1 {
		log.Fatal("invalid parameters")
	}

	if *encryptionRatio < 0 || *encryptionRatio > 1 {
		log.Fatal("encryption ratio must be between 0 and 1")
	}

	config := LoadTestConfig{
		NumMessages:     *numMessages,
		NumRecipients:   *numRecipients,
		EncryptionRatio: *encryptionRatio,
		Concurrent:      *concurrent,
		Verbose:         *verbose,
	}

	tester := NewLoadTester(config)
	tester.Run()
}
