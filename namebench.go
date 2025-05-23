package main

import (
	"bufio"
	"context" // Added for context.Background()
	"flag"
	"fmt" // Added for fmt.Errorf
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort" // Added for sorting results
	"strings"
	"time" // Added for time.Duration

	"github.com/google/namebench/dnsqueue"
	"github.com/google/namebench/history"
	"github.com/google/namebench/ui"
)

// UI flags
var nw_path = flag.String("nw_path", "/Applications/node-webkit.app/Contents/MacOS/node-webkit", "Path to nodejs-webkit binary for UI mode")
var nw_package = flag.String("nw_package", "./ui/app.nw", "Path to nodejs-webkit package for UI mode")
var port = flag.Int("port", 0, "Port to listen on for UI mode")

// CLI flags
var cli = flag.Bool("cli", false, "Enable command-line interface mode")
var nameservers = flag.String("nameservers", "", "Comma-separated IP[:port] of nameservers to benchmark (e.g., 8.8.8.8,1.1.1.1:5353)")
var domain_source = flag.String("domain_source", "history", "Source for domains: 'history', 'default_list', or a filepath")
var count = flag.Int("count", 20, "Number of unique domains to test")
var record_type = flag.String("record_type", "A", "DNS record type to query (e.g., A, AAAA, MX)")
var dnssec = flag.Bool("dnssec", false, "Enable DNSSEC (DO bit) in queries. Note: dnsqueue.Request needs update for this to be effective.")

// Global defaults
var defaultNameservers = []string{"8.8.8.8:53", "1.1.1.1:53", "9.9.9.9:53"}
var defaultDomains = []string{
	"google.com", "cloudflare.com", "amazon.com", "wikipedia.org", "twitter.com",
	"facebook.com", "youtube.com", "instagram.com", "linkedin.com", "netflix.com",
}

// openWindow opens a nodejs-webkit window, and points it at the given URL.
func openWindow(url string) (err error) {
	// Pre-check that nw_path exists and is a file
	if _, err := os.Stat(*nw_path); os.IsNotExist(err) {
		newErr := fmt.Errorf("node-webkit binary not found at %s, please check path or installation: %w", *nw_path, err)
		log.Println(newErr) // Log the specific error
		return newErr
	} else if err != nil { // Other error with os.Stat (e.g. permission denied)
		newErr := fmt.Errorf("error checking node-webkit binary at %s: %w", *nw_path, err)
		log.Println(newErr)
		return newErr
	}

	// Pre-check that nw_package exists and is a file (or directory, depending on how nw.js uses it)
	// For app.nw, it's typically a zip file or a directory. Stat is fine.
	if _, err := os.Stat(*nw_package); os.IsNotExist(err) {
		newErr := fmt.Errorf("node-webkit package not found at %s: %w", *nw_package, err)
		log.Println(newErr) // Log the specific error
		return newErr
	} else if err != nil { // Other error with os.Stat
		newErr := fmt.Errorf("error checking node-webkit package at %s: %w", *nw_package, err)
		log.Println(newErr)
		return newErr
	}

	os.Setenv("APP_URL", url)
	cmd := exec.Command(*nw_path, *nw_package)
	if err := cmd.Run(); err != nil {
		// Log the error from cmd.Run itself, as it might be different from the pre-checks
		log.Printf("error running node-webkit command %s with package %s: %v", *nw_path, *nw_package, err)
		// Wrap error from cmd.Run for the return value
		return fmt.Errorf("error running node-webkit command %s with package %s: %w", *nw_path, *nw_package, err)
	}
	return nil // Explicitly return nil on success
}

func main() {
	flag.Parse()

	if *cli {
		runCliBenchmark()
	} else {
		// Existing UI server logic
		ui.RegisterHandlers()
		if *port != 0 {
			log.Printf("UI Mode: Listening at :%d", *port)
			err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
			if err != nil {
				// Wrap error from http.ListenAndServe
				log.Fatalf("UI Mode: Failed to listen on %d: %v", *port, fmt.Errorf("ListenAndServe failed: %w", err))
			}
		} else {
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				// Wrap error from net.Listen
				log.Fatalf("UI Mode: Failed to listen on dynamic port: %v", fmt.Errorf("net.Listen failed: %w", err))
			}
			url := fmt.Sprintf("http://%s/", listener.Addr().String())
			log.Printf("UI Mode: URL: %s", url)
			log.Printf("UI Mode: Launching node-webkit UI...")
			go func() {
				if err := openWindow(url); err != nil {
					log.Printf("Error opening node-webkit window: %v", err) // Log wrapped error
				}
			}()
			// http.Serve error is critical, so panic with wrapped error
			if err := http.Serve(listener, nil); err != nil {
				panic(fmt.Errorf("http.Serve failed: %w", err))
			}
		}
	}
}

func runCliBenchmark() {
	log.Println("Namebench CLI mode started.")

	// 1. Process nameservers
	currentNameservers := parseNameservers(*nameservers, defaultNameservers)
	if len(currentNameservers) == 0 {
		log.Fatalf("No nameservers to test. Exiting.") // No error to wrap here, it's a configuration issue
		return
	}
	log.Printf("Using nameservers: %v", currentNameservers)

	// 2. Process domains
	var domainsToTest []string
	log.Printf("Domain source: '%s'", *domain_source)
	switch *domain_source {
	case "history":
		log.Println("Attempting to read domains from Chrome history...")
		historyRecords, err := history.Chrome(ui.HISTORY_DAYS) // ui.HISTORY_DAYS is a const like 30
		if err != nil {
			// Log the wrapped error from history.Chrome
			log.Printf("Error reading Chrome history (will fall back to default list): %v", err)
			domainsToTest = defaultDomains
		} else if len(historyRecords) == 0 {
			log.Println("No domains found in Chrome history. Falling back to default domain list.")
			domainsToTest = defaultDomains
		} else {
			domainsToTest = history.Uniq(history.ExternalHostnames(historyRecords))
			if len(domainsToTest) == 0 {
				log.Println("No external hostnames found in Chrome history. Falling back to default domain list.")
				domainsToTest = defaultDomains
			} else {
				log.Printf("Successfully loaded %d unique external domains from history.", len(domainsToTest))
			}
		}
	case "default_list":
		log.Println("Using the default domain list.")
		domainsToTest = defaultDomains
	default: // Filepath
		log.Printf("Attempting to read domains from file: %s", *domain_source)
		loadedFileDomains, err := loadDomainsFromFile(*domain_source)
		if err != nil {
			// Log the wrapped error from loadDomainsFromFile
			log.Printf("Error loading domains from file '%s' (will fall back to default list): %v", *domain_source, err)
			domainsToTest = defaultDomains
		} else if len(loadedFileDomains) == 0 {
			log.Printf("No domains found in file '%s'. Falling back to default domain list.", *domain_source)
			domainsToTest = defaultDomains
		} else {
			domainsToTest = loadedFileDomains
			log.Printf("Successfully loaded %d domains from file '%s'.", len(domainsToTest), *domain_source)
		}
	}

	if len(domainsToTest) == 0 {
		log.Fatalf("No domains to test after processing source '%s'. Exiting.", *domain_source) // Configuration issue
		return
	}

	// Select -count unique domains
	selectedDomains := history.Random(*count, domainsToTest)
	if len(selectedDomains) == 0 {
		log.Fatalf("Could not select any domains for testing (requested %d from a pool of %d). Exiting.", *count, len(domainsToTest)) // Configuration issue
		return
	}
	log.Printf("Selected %d unique domains for testing: %v", len(selectedDomains), selectedDomains)

	log.Printf("Starting benchmark with record type '%s'. DNSSEC flag: %t.", *record_type, *dnssec)

	// 3. Benchmarking Loop
	allResults := make(map[string][]dnsqueue.Result) // Key: nameserver string
	benchmarkCtx := context.Background()             // Root context for this benchmark run

	for _, ns := range currentNameservers {
		log.Printf("--------------------------------------------------")
		log.Printf("Testing nameserver: %s", ns)
		log.Printf("--------------------------------------------------")
		q := dnsqueue.StartQueue(ui.QUEUE_LENGTH, ui.WORKERS)

		for _, domain := range selectedDomains {
			req := &dnsqueue.Request{
				Ctx:             benchmarkCtx, // Pass context
				Destination:     ns,
				RecordType:      *record_type,
				RecordName:      domain + ".", // Ensure trailing dot for FQDN
				VerifySignature: *dnssec,
			}
			q.Requests <- req // Send the request directly to the channel
		}
		q.SendCompletionSignal()

		answered := 0
		var currentNsResults []dnsqueue.Result
		for {
			if answered == len(selectedDomains) {
				break
			}
			result := <-q.Results
			currentNsResults = append(currentNsResults, result)
			answered++
			if result.Error != "" {
				// Log the error string from result.Error (already wrapped by dnsqueue.SendQuery)
				log.Printf("Query for %s -> %s: Error: %s", result.Request.RecordName, result.Request.Destination, result.Error)
			}
		}
		allResults[ns] = currentNsResults
		
		// Calculate and log average for this nameserver (logging already exists)
		var totalDuration time.Duration
		var successfulQueries int
		for _, r := range currentNsResults {
			if r.Error == "" {
				totalDuration += r.Duration
				successfulQueries++
			}
		}
		if successfulQueries > 0 {
			log.Printf("Finished testing %s: Avg Duration: %s (%d/%d successful queries)", ns, totalDuration/time.Duration(successfulQueries), successfulQueries, len(selectedDomains))
		} else {
			log.Printf("Finished testing %s: No successful queries (%d attempts)", ns, len(selectedDomains))
		}
	}
	log.Println("--------------------------------------------------")
	log.Println("CLI benchmark finished. Processing results...")
	log.Println("--------------------------------------------------")

	// 2. Formatted Printing of Detailed Results (code from previous step, assumed correct)
	fmt.Println("\nNamebench CLI Mode Results")
	fmt.Println("==========================")

	sortedNameservers := make([]string, 0, len(allResults))
	for ns := range allResults {
		sortedNameservers = append(sortedNameservers, ns)
	}
	sort.Strings(sortedNameservers)

	type SummaryEntry struct {
		Nameserver         string
		AverageMs        float64
		SuccessfulQueries int
		TotalQueries      int
	}
	var summaryData []SummaryEntry

	for _, ns := range sortedNameservers {
		fmt.Printf("\nNameserver: %s\n", ns)
		fmt.Println("--------------------------")
		resultsForNs := allResults[ns]
		var nsTotalDuration time.Duration
		var nsSuccessfulQueries int

		for _, result := range resultsForNs {
			fmt.Printf("  Domain: %s, Time: %s", result.Request.RecordName, result.Duration)
			if result.Error != "" {
				fmt.Printf(", Error: %s\n", result.Error) // Error string is already wrapped
			} else {
				fmt.Println()
				nsSuccessfulQueries++
				nsTotalDuration += result.Duration
			}
		}

		var avgMs float64
		if nsSuccessfulQueries > 0 {
			avgMs = float64(nsTotalDuration.Nanoseconds()/1e6) / float64(nsSuccessfulQueries)
			fmt.Printf("  Average Response Time: %.2f ms\n", avgMs)
		} else {
			fmt.Println("  Average Response Time: N/A (no successful queries)")
		}
		fmt.Printf("  Successful Queries: %d/%d\n", nsSuccessfulQueries, len(resultsForNs))
		if *dnssec {
			fmt.Println("  DNSSEC: Test enabled (DO bit set in queries)")
		}
		summaryData = append(summaryData, SummaryEntry{
			Nameserver:         ns,
			AverageMs:        avgMs,
			SuccessfulQueries: nsSuccessfulQueries,
			TotalQueries:      len(resultsForNs),
		})
	}

	// 3. Summary Section (code from previous step, assumed correct)
	fmt.Println("\nSummary:")
	fmt.Println("=======")

	sort.Slice(summaryData, func(i, j int) bool {
		if summaryData[i].AverageMs == summaryData[j].AverageMs {
			return summaryData[i].SuccessfulQueries > summaryData[j].SuccessfulQueries
		}
		if summaryData[i].AverageMs == 0 && summaryData[j].AverageMs > 0 {
			return false
		}
		if summaryData[j].AverageMs == 0 && summaryData[i].AverageMs > 0 {
			return true
		}
		return summaryData[i].AverageMs < summaryData[j].AverageMs
	})

	fmt.Println("Ranked Nameservers (Fastest to Slowest):")
	for i, entry := range summaryData {
		fmt.Printf("%d. %s: Avg Response: %.2f ms, Success: %d/%d\n",
			i+1, entry.Nameserver, entry.AverageMs, entry.SuccessfulQueries, entry.TotalQueries)
	}

	if len(summaryData) > 0 {
		fastestAvg := summaryData[0].AverageMs
		fmt.Println("\nFastest Nameserver(s):")
		for _, entry := range summaryData {
			if entry.AverageMs == fastestAvg && entry.SuccessfulQueries > 0 {
				fmt.Printf("- %s (Avg: %.2f ms, Success: %d/%d)\n",
					entry.Nameserver, entry.AverageMs, entry.SuccessfulQueries, entry.TotalQueries)
			} else if entry.AverageMs > fastestAvg && entry.AverageMs != 0 {
				break
			}
		}
	} else {
		fmt.Println("No benchmark data to summarize.")
	}
}

// parseNameservers processes the nameservers flag string and returns a list of nameserver addresses.
func parseNameservers(nsFlag string, defaultNS []string) []string {
	var parsed []string
	if nsFlag == "" {
		log.Printf("No nameservers specified via flag, using defaults: %v", defaultNS)
		return defaultNS
	}

	nsParts := strings.Split(nsFlag, ",")
	for _, ns := range nsParts {
		ns = strings.TrimSpace(ns)
		if ns == "" {
			continue
		}
		if !strings.Contains(ns, ":") {
			ns = ns + ":53" // Append default port if missing
		}
		parsed = append(parsed, ns)
	}
	if len(parsed) == 0 {
		log.Printf("Nameserver flag processing resulted in empty list, using defaults: %v", defaultNS)
		return defaultNS
	}
	return parsed
}

// loadDomainsFromFile reads domains from a given filepath, one domain per line.
// It trims whitespace from each line and skips empty lines.
func loadDomainsFromFile(filePath string) ([]string, error) {
	var domains []string
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open domain file %s: %w", filePath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		domain := strings.TrimSpace(scanner.Text())
		if domain != "" {
			domains = append(domains, domain)
		}
	}
	if err := scanner.Err(); err != nil {
		// Wrap scanner.Err()
		return domains, fmt.Errorf("error scanning domain file %s: %w", filePath, err)
	}
	return domains, nil
}
