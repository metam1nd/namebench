package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"bufio"

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
	os.Setenv("APP_URL", url)
	cmd := exec.Command(*nw_path, *nw_package)
	if err := cmd.Run(); err != nil {
		log.Printf("error running %s %s: %s", *nw_path, *nw_package, err)
		return err
	}
	return
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
				log.Fatalf("UI Mode: Failed to listen on %d: %s", *port, err)
			}
		} else {
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				log.Fatalf("UI Mode: Failed to listen: %s", err)
			}
			url := fmt.Sprintf("http://%s/", listener.Addr().String())
			log.Printf("UI Mode: URL: %s", url)
			log.Printf("UI Mode: Launching node-webkit UI...")
			go openWindow(url)
			panic(http.Serve(listener, nil))
		}
	}
}

func runCliBenchmark() {
	log.Println("Namebench CLI mode started.")

	// 1. Process nameservers
	currentNameservers := parseNameservers(*nameservers, defaultNameservers)
	if len(currentNameservers) == 0 {
		log.Fatalf("No nameservers to test. Exiting.")
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
			log.Printf("Error reading Chrome history: %v. Falling back to default domain list.", err)
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
			log.Printf("Error loading domains from file '%s': %v. Falling back to default domain list.", *domain_source, err)
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
		log.Fatalf("No domains to test after processing source '%s'. Exiting.", *domain_source)
		return
	}

	// Select -count unique domains
	selectedDomains := history.Random(*count, domainsToTest)
	if len(selectedDomains) == 0 {
		log.Fatalf("Could not select any domains for testing (requested %d from a pool of %d). Exiting.", *count, len(domainsToTest))
		return
	}
	log.Printf("Selected %d unique domains for testing: %v", len(selectedDomains), selectedDomains)

	log.Printf("Starting benchmark with record type '%s'. DNSSEC flag: %t (Note: effective DNSSEC support depends on dnsqueue.Request update).", *record_type, *dnssec)

	// 3. Benchmarking Loop
	allResults := make(map[string][]dnsqueue.Result) // Key: nameserver string

	for _, ns := range currentNameservers {
		log.Printf("--------------------------------------------------")
		log.Printf("Testing nameserver: %s", ns)
		log.Printf("--------------------------------------------------")
		// Using ui.QUEUE_LENGTH and ui.WORKERS for consistency with UI mode for now.
		q := dnsqueue.StartQueue(ui.QUEUE_LENGTH, ui.WORKERS)

		for _, domain := range selectedDomains {
			// The dnsqueue.Request struct needs to be updated to include VerifySignature field
			// and dnsqueue.SendQuery needs to use it.
			// For now, we are calling q.Add which creates a request without VerifySignature.
			// This will be addressed in a subsequent step.
			req := &dnsqueue.Request{
				Destination:     ns,
				RecordType:      *record_type,
				RecordName:      domain + ".", // Ensure trailing dot for FQDN
				VerifySignature: *dnssec,
			}
			q.Requests <- req // Send the request directly to the channel
		}
		q.SendCompletionSignal()

		answered := 0
		var totalDuration time.Duration
		var successfulQueries int
		var currentNsResults []dnsqueue.Result
		for {
			if answered == len(selectedDomains) {
				break
			}
			result := <-q.Results
			currentNsResults = append(currentNsResults, result) // Store result
			answered++
			// Logging during benchmark can be reduced if too verbose, will be printed later
			if result.Error != "" {
				log.Printf("Query for %s -> %s: Error: %s", result.Request.RecordName, result.Request.Destination, result.Error)
			} else {
				// log.Printf("Query for %s -> %s: Duration: %s, Answers: %d", result.Request.RecordName, result.Request.Destination, result.Duration, len(result.Answers))
				totalDuration += result.Duration
				successfulQueries++
			}
		}
		allResults[ns] = currentNsResults // Store all results for this nameserver

		if successfulQueries > 0 {
			log.Printf("Finished testing %s: Avg Duration: %s (%d/%d successful queries)", ns, totalDuration/time.Duration(successfulQueries), successfulQueries, len(selectedDomains))
		} else {
			log.Printf("Finished testing %s: No successful queries (%d attempts)", ns, len(selectedDomains))
		}
	}
	log.Println("--------------------------------------------------")
	log.Println("CLI benchmark finished. Processing results...")
	log.Println("--------------------------------------------------")

	// 2. Formatted Printing of Detailed Results
	fmt.Println("\nNamebench CLI Mode Results")
	fmt.Println("==========================")

	// Sort nameservers for consistent output order
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
				fmt.Printf(", Error: %s\n", result.Error)
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

	// 3. Summary Section
	fmt.Println("\nSummary:")
	fmt.Println("=======")

	// Sort summaryData: primarily by AverageMs (asc), secondarily by SuccessfulQueries (desc)
	sort.Slice(summaryData, func(i, j int) bool {
		if summaryData[i].AverageMs == summaryData[j].AverageMs {
			return summaryData[i].SuccessfulQueries > summaryData[j].SuccessfulQueries
		}
		// Handle cases where AverageMs might be 0 (no successful queries)
		if summaryData[i].AverageMs == 0 && summaryData[j].AverageMs > 0 { // Treat 0 as "worse" than any positive avg
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
		// Identify fastest based on sorted list (could be multiple if ties)
		fastestAvg := summaryData[0].AverageMs
		fmt.Println("\nFastest Nameserver(s):")
		for _, entry := range summaryData {
			if entry.AverageMs == fastestAvg && entry.SuccessfulQueries > 0 { // Ensure it had successful queries
				fmt.Printf("- %s (Avg: %.2f ms, Success: %d/%d)\n",
					entry.Nameserver, entry.AverageMs, entry.SuccessfulQueries, entry.TotalQueries)
			} else if entry.AverageMs > fastestAvg && entry.AverageMs != 0 { // Stop if we are past the fastest tier
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
	if len(parsed) == 0 { // Should not happen if nsFlag is not empty, but as a safeguard
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
		return nil, fmt.Errorf("opening domain file '%s': %w", filePath, err)
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
		return nil, fmt.Errorf("reading domain file '%s': %w", filePath, err)
	}
	return domains, nil
}
