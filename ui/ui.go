// The ui package contains methods for handling UI URL's.
package ui

import (
	"context" // Added for context.Background()
	"fmt"     // For error wrapping
	"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/google/namebench/dnschecks"
	"github.com/google/namebench/dnsqueue"
	"github.com/google/namebench/history"
)

const (
	// How many requests/responses can be queued at once
	QUEUE_LENGTH = 65535

	// Number of workers (same as Chrome's DNS prefetch queue)
	WORKERS = 8

	// Number of tests to run
	COUNT = 50

	// How far back to reach into browser history
	HISTORY_DAYS = 30
)

var (
	indexTmpl = loadTemplate("ui/templates/index.html")
)

// RegisterHandler registers all known handlers.
func RegisterHandlers() {
	http.HandleFunc("/", Index)
	http.Handle("/static/", http.StripPrefix("/static", http.FileServer(http.Dir("ui/static"))))
	http.HandleFunc("/submit", Submit)
	http.HandleFunc("/dnssec", DnsSec)
}

// loadTemplate loads a set of templates.
func loadTemplate(paths ...string) *template.Template {
	t := template.New(strings.Join(paths, ",")) // Use the combined path as the template name for clarity
	// The actual parsing happens with ParseFiles, which takes individual paths.
	// The name given to template.New() is primarily for identification if multiple named templates are involved.
	// For a single, encompassing template, this is fine.
	_, err := t.ParseFiles(paths...)
	if err != nil {
		// Panic is the existing behavior, wrap error for more context.
		panic(fmt.Errorf("failed to parse template files %s: %w", strings.Join(paths, ", "), err))
	}
	return t
}

// Index handles /
func Index(w http.ResponseWriter, r *http.Request) {
	if err := indexTmpl.ExecuteTemplate(w, "index.html", nil); err != nil {
		// Log the detailed wrapped error for server-side diagnosis
		log.Printf("Error executing template index.html: %v", fmt.Errorf("template execution failed: %w", err))
		// Provide a generic error to the client
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
	return
}

// DnsSec handles /dnssec
func DnsSec(w http.ResponseWriter, r *http.Request) {
	servers := []string{
		"8.8.8.8:53",
		"75.75.75.75:53",
		"4.2.2.1:53",
		"208.67.222.222:53",
	}
	for _, ip := range servers {
		result, err := dnschecks.DnsSec(ip) // Assuming DnsSec might return an error
		if err != nil {
			log.Printf("Error checking DNSSEC for %s: %v", ip, err) // Log the error
			// Depending on desired behavior, might want to inform client or just log
			// For now, just log and continue.
			continue
		}
		log.Printf("%s DNSSEC: %t", ip, result) // Changed from %s to %t for boolean
	}
	// Consider sending a meaningful response to the client, e.g., a JSON summary.
	// For now, it logs to server and returns an empty 200 OK.
	fmt.Fprintln(w, "DNSSEC checks logged to server.")
}

// Submit handles /submit
func Submit(w http.ResponseWriter, r *http.Request) {
	records, err := history.Chrome(HISTORY_DAYS)
	if err != nil {
		// Panic is the existing behavior, wrap error for more context.
		panic(fmt.Errorf("failed to get Chrome history: %w", err))
	}

	q := dnsqueue.StartQueue(QUEUE_LENGTH, WORKERS)
	hostnames := history.Random(COUNT, history.Uniq(history.ExternalHostnames(records)))
	uiCtx := context.Background() // Context for UI-initiated requests

	for _, record := range hostnames {
		// The updated q.Add now takes: ctx, dest, record_type, record_name, verifySignature
		// For UI-originated requests, verifySignature is false.
		q.Add(uiCtx, "8.8.8.8:53", "A", record+".", false)
		log.Printf("Added %s", record)
	}
	q.SendCompletionSignal() // Close the request channel

	answered := 0
	for {
		if answered == len(hostnames) {
			break
		}
		result := <-q.Results // Read from the results channel
		answered++
		// It's good to log the result.Error if it's not empty
		if result.Error != "" {
			log.Printf("DNS query for %s (via UI) resulted in error: %s", result.Request.RecordName, result.Error)
		} else {
			// log.Printf("DNS query for %s (via UI) successful, duration: %s", result.Request.RecordName, result.Duration) // Can be verbose
		}
	}
	// Consider sending a meaningful response to the client upon completion.
	log.Println("UI initiated benchmark run completed.")
	fmt.Fprintln(w, "Benchmark run completed. Results logged to server.")
	return
}
