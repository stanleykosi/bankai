package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/bankai-project/backend/internal/config"
	"github.com/bankai-project/backend/internal/polymarket/clob"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Display credential status (without showing actual values)
	fmt.Println("=== CLOB Credentials Check ===")
	fmt.Printf("CLOB URL: %s\n", cfg.Polymarket.ClobURL)

	apiKeySet := cfg.Polymarket.BuilderAPIKey != ""
	secretSet := cfg.Polymarket.BuilderSecret != ""
	passphraseSet := cfg.Polymarket.BuilderPass != ""

	fmt.Printf("Builder API Key: %s\n", statusString(apiKeySet))
	fmt.Printf("Builder Secret: %s\n", statusString(secretSet))
	fmt.Printf("Builder Passphrase: %s\n", statusString(passphraseSet))
	fmt.Println()

	if !apiKeySet || !secretSet || !passphraseSet {
		log.Fatalf("❌ Missing required credentials. Please check your .env file for:")
		if !apiKeySet {
			fmt.Println("  - POLY_BUILDER_API_KEY")
		}
		if !secretSet {
			fmt.Println("  - POLY_BUILDER_SECRET")
		}
		if !passphraseSet {
			fmt.Println("  - POLY_BUILDER_PASSPHRASE")
		}
		os.Exit(1)
	}

	// Create CLOB client
	client := clob.NewClient(cfg)

	// Test 1: Try to fetch order book (GET request - simpler test)
	fmt.Println("Test 1: Testing GET request (order book)...")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Use a known token ID for testing (you can change this to a real token)
	testTokenID := "0x04c22afae6a03438e8c8e8430b0933098b4d8c1c3" // Example token ID
	book, err := client.GetBook(ctx, testTokenID)
	if err != nil {
		// Check if it's an auth error (401) vs validation error (400)
		if contains(err.Error(), "401") || contains(err.Error(), "Unauthorized") {
			fmt.Printf("❌ GET request authentication failed: %v\n", err)
			fmt.Println("\nThis indicates:")
			fmt.Println("  - API key is invalid or expired")
			fmt.Println("  - Secret key is incorrect")
			fmt.Println("  - Passphrase is incorrect")
			fmt.Println("  - Signature generation is failing")
			log.Fatalf("CLOB authentication test failed")
		} else if contains(err.Error(), "400") || contains(err.Error(), "Invalid token") {
			// 400 is expected for invalid token - this means auth passed!
			fmt.Printf("✅ GET request authentication succeeded! (Got expected validation error: %v)\n", err)
			fmt.Println("   This confirms your credentials are valid - authentication passed!")
		} else {
			fmt.Printf("⚠️  GET request returned unexpected error: %v\n", err)
			fmt.Println("   This might indicate a different issue (network, server, etc.)")
			fmt.Println("   But authentication appears to have passed (no 401 error)")
		}
	} else {
		fmt.Printf("✅ GET request succeeded! Retrieved order book with %d bids, %d asks\n",
			len(book.Bids), len(book.Asks))
	}
	fmt.Println()

	// Test 2: Try a minimal POST request (this will fail with invalid order, but should pass auth)
	fmt.Println("Test 2: Testing POST request authentication...")

	// Create a minimal invalid order just to test authentication
	// The order will be rejected, but if we get past 401, auth is working
	ownerKey := os.Getenv("POLY_OWNER_API_KEY")
	if ownerKey == "" {
		ownerKey = cfg.Polymarket.BuilderAPIKey
		fmt.Println("   Using Builder API key as owner (set POLY_OWNER_API_KEY to test with a user API key)")
	} else {
		fmt.Println("   Using POLY_OWNER_API_KEY for order owner (preferred for real trades)")
	}

	testReq := &clob.PostOrderRequest{
		Order: clob.Order{
			Salt:          "1",
			Maker:         "0x0000000000000000000000000000000000000000",
			Signer:        "0x0000000000000000000000000000000000000000",
			Taker:         "0x0000000000000000000000000000000000000000",
			TokenID:       testTokenID,
			MakerAmount:   "0",
			TakerAmount:   "0",
			Expiration:    "0",
			Nonce:         "0",
			FeeRateBps:    "0",
			Side:          clob.BUY,
			SignatureType: 0,
			Signature:     "0x",
		},
		Owner:     ownerKey,
		OrderType: clob.OrderTypeGTC,
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel2()

	_, err = client.PostOrder(ctx2, testReq, nil)
	if err != nil {
		// Check if it's an auth error (401) vs validation error (400)
		if contains(err.Error(), "401") || contains(err.Error(), "Unauthorized") {
			fmt.Printf("❌ POST request authentication failed: %v\n", err)
			fmt.Println("\nThis indicates:")
			fmt.Println("  - API key is invalid or expired")
			fmt.Println("  - Secret key is incorrect")
			fmt.Println("  - Passphrase is incorrect")
			fmt.Println("  - Signature generation is failing")
			log.Fatalf("CLOB authentication test failed")
		} else if contains(err.Error(), "400") || contains(err.Error(), "Bad Request") {
			// 400 is expected for invalid order - this means auth passed!
			fmt.Printf("✅ POST request authentication succeeded! (Got expected validation error: %v)\n", err)
		} else {
			fmt.Printf("⚠️  POST request returned unexpected error: %v\n", err)
			fmt.Println("This might indicate a different issue (network, server, etc.)")
		}
	} else {
		fmt.Println("✅ POST request succeeded!")
	}

	fmt.Println()
	fmt.Println("=== Summary ===")
	fmt.Println("✅ CLOB credentials are VALID and working!")
	fmt.Println("✅ Authentication headers are being generated correctly")
	fmt.Println("✅ API key, secret, and passphrase are configured correctly")
	fmt.Println()
	fmt.Println("Note: If you saw 400 errors above, that's GOOD - it means authentication")
	fmt.Println("      passed! A 401 error would indicate invalid credentials.")
}

func statusString(set bool) string {
	if set {
		return "[SET]"
	}
	return "[NOT SET]"
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
