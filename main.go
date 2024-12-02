package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cloudflare/cloudflare-go"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Global variables
var (
	apiToken   string
	zoneName   string
	recordName string
)

// init function for Viper configuration
func init() {
	// Bind environment variables and flags using Viper
	viper.SetEnvPrefix("cf")
	viper.AutomaticEnv()

	// Set up flags using pflag (which Viper uses for flag handling)
	pflag.String("api-token", "", "Cloudflare API Token")
	pflag.String("zone-name", "", "Cloudflare Zone Name")
	pflag.String("record-name", "", "DNS Record Name")
	pflag.Parse()

	// Bind flags to Viper
	viper.BindPFlags(pflag.CommandLine)

	// Read environment variables if flags are not provided
	apiToken = viper.GetString("api-token")
	zoneName = viper.GetString("zone-name")
	recordName = viper.GetString("record-name")

	// Validate required fields
	if apiToken == "" || zoneName == "" || recordName == "" {
		fmt.Println("Missing required flags or environment variables:")
		fmt.Println("CLOUDFLARE_API_TOKEN (or --api-token)")
		fmt.Println("CLOUDFLARE_ZONE_NAME (or --zone-name)")
		fmt.Println("CLOUDFLARE_RECORD_NAME (or --record-name)")
		os.Exit(1)
	}

	// Print help if no arguments or flags provided
	if len(os.Args) < 2 {
		pflag.PrintDefaults()
		os.Exit(0)
	}
}

func main() {
	// Initialize Cloudflare API client
	api, err := cloudflare.NewWithAPIToken(apiToken)
	if err != nil {
		log.Fatalf("Error initializing Cloudflare API: %v", err)
	}

	// Fetch the Zone ID
	zoneID, err := api.ZoneIDByName(zoneName)
	if err != nil {
		log.Fatalf("Error fetching Zone ID for %s: %v", zoneName, err)
	}

	zone := &cloudflare.ResourceContainer{
		Level:      cloudflare.ZoneRouteLevel,
		Identifier: zoneID,
	}

	// Create a context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Fetch public IP
	ip, err := getPublicIP(ctx)
	if err != nil {
		log.Fatalf("Error fetching public IP: %v", err)
	}

	fmt.Println("Your IP address is ", ip)

	// List DNS records with the correct container type
	records, resultInfo, err := api.ListDNSRecords(ctx, zone, cloudflare.ListDNSRecordsParams{
		Name: recordName,
		Type: "A",
	})
	if err != nil {
		log.Fatalf("Error fetching DNS records: %v", err)
	}

	// Optionally, log the resultInfo (for pagination or additional metadata)
	fmt.Printf("Total records found: %d\n", resultInfo.Total)

	// Update the DNS record
	if len(records) > 0 {
		record := records[0] // Assuming we are working with the first matching record

		// Check if the IP address needs to be updated
		if record.Content == ip {
			fmt.Printf("DNS record already up-to-date for %s: %s\n", recordName, ip)
			return
		}

		record.Content = ip
		record, err = api.UpdateDNSRecord(ctx, zone, cloudflare.UpdateDNSRecordParams{
			Type:    record.Type,
			Name:    record.Name,
			Content: record.Content,
			TTL:     record.TTL,
			Proxied: record.Proxied,
		})
		if err != nil {
			log.Fatalf("Error updating DNS record: %v", err)
		}

		fmt.Printf("Successfully updated DNS record for %s to %s\n", recordName, ip)
	} else {
		log.Fatalf("No A records found for %s", recordName)
	}

	if len(records) == 0 {
		log.Fatalf("No A records found for %s", recordName)
	}

	fmt.Printf("Successfully updated DNS record for %s to %s\n", recordName, ip)
}

// getPublicIP retrieves the public IPv4 address from multiple services
func getPublicIP(ctx context.Context) (string, error) {
	services := []string{
		"https://checkip.amazonaws.com",
		"https://icanhazip.com",
	}

	type result struct {
		ip  string
		err error
	}

	results := make(chan result, len(services))
	for _, url := range services {
		go func(service string) {
			ip, err := fetchIP(ctx, service)
			results <- result{ip, err}
		}(url)
	}

	var ips []string
	for range services {
		res := <-results
		if res.err == nil {
			ips = append(ips, res.ip)
		}
	}

	if len(ips) == 0 {
		return "", fmt.Errorf("failed to fetch public IP from all services")
	}

	if len(ips) > 1 && ips[0] != ips[1] {
		log.Printf("Warning: IP mismatch between services: %s vs %s. Using %s", ips[0], ips[1], ips[0])
	}

	return ips[0], nil
}

// fetchIP fetches the public IP from a single service
func fetchIP(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	ip := strings.TrimSpace(string(body))
	if ip == "" {
		return "", fmt.Errorf("received empty IP address from %s", url)
	}

	return ip, nil
}
