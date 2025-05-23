// the history package is a collection of functions for reading history files from browsers.
package history

import (
	"database/sql"
	"fmt"
	_ "github.com/mattn/go-sqlite3" // SQLite3 driver
	"io"
	"io/ioutil"
	"log"
	"os"
	"strings"      // For Uniq and ExternalHostnames
	"math/rand"    // For Random
	"net/url"      // For ExternalHostnames
	psl "golang.org/x/net/publicsuffix" // For ExternalHostnames
)

// unlockDatabase is a bad hack for opening potentially locked SQLite databases.
func unlockDatabase(path string) (unlocked_path string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open database %s: %w", path, err)
	}
	defer f.Close()

	t, err := ioutil.TempFile("", "namebench-history-*.db") // Added prefix for clarity
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	// Defer a function to handle closing and potential removal of the temp file
	// This ensures cleanup even if later operations fail.
	defer func() {
		if closeErr := t.Close(); closeErr != nil && err == nil {
			// If we haven't already set an error, set one for the close failure.
			// This is tricky because we might be shadowing the original `err` if not careful.
			// For simplicity, just log it if we are already returning another error.
			log.Printf("Error closing temp file %s: %v", t.Name(), closeErr)
			if err == nil { // only assign if no other error has occurred.
			    err = fmt.Errorf("failed to close temp file %s: %w", t.Name(), closeErr)
			    // If closing failed, attempt to remove the temp file, but prioritize returning the close error.
			    if removeErr := os.Remove(t.Name()); removeErr != nil {
				    log.Printf("Failed to remove temp file %s after close error: %v", t.Name(), removeErr)
			    }
            }
		}
		// If the main function 'err' is not nil (meaning an error occurred during copy or other ops),
		// we should remove the temp file because it might be corrupted or incomplete.
		if err != nil {
			if removeErr := os.Remove(t.Name()); removeErr != nil {
				log.Printf("Failed to remove temp file %s after error: %v", t.Name(), removeErr)
			}
		}
	}()


	written, err := io.Copy(t, f)
	if err != nil {
		return "", fmt.Errorf("failed to copy database from %s to %s: %w", path, t.Name(), err)
	}
	// The file handle t is closed by the deferred function.

	log.Printf("%d bytes written from %s to %s", written, path, t.Name())
	return t.Name(), nil // err is nil here if copy succeeded and close (from defer) doesn't override.
}

// Chrome returns an array of URLs found in Chrome's history within X days
func Chrome(days int) (urls []string, err error) {
	paths := []string{
		"${HOME}/Library/Application Support/Google/Chrome/Default/History",
		"${HOME}/.config/google-chrome/Default/History",
		"${APPDATA}/Google/Chrome/User Data/Default/History",
		"${USERPROFILE}/Local Settings/Application Data/Google/Chrome/User Data/Default/History",
	}

	query := fmt.Sprintf(
		`SELECT urls.url FROM visits
		 LEFT JOIN urls ON visits.url = urls.id
		 WHERE (visit_time - 11644473600000000 >
			    strftime('%%s', date('now', '-%d day')) * 1000000)
		 ORDER BY visit_time DESC`, days)

	var lastErr error
	for _, p := range paths {
		path := os.ExpandEnv(p)
		// log.Printf("Checking %s", path) // Can be verbose
		_, statErr := os.Stat(path)
		if statErr != nil {
			// log.Printf("Skipping history path %s: %v", path, statErr)
			lastErr = fmt.Errorf("history path %s not found: %w", path, statErr)
			continue
		}

		unlocked_path, unlockErr := unlockDatabase(path)
		if unlockErr != nil {
			log.Printf("Failed to unlock database %s: %v. Trying next path.", path, unlockErr)
			lastErr = fmt.Errorf("failed to unlock database %s: %w", path, unlockErr)
			continue
		}
		// Ensure temp file is cleaned up whether sql.Open succeeds or fails
		defer os.Remove(unlocked_path)

		db, openErr := sql.Open("sqlite3", unlocked_path)
		if openErr != nil {
			log.Printf("Failed to open SQLite database %s: %v. Trying next path.", unlocked_path, openErr)
			lastErr = fmt.Errorf("failed to open sqlite database %s: %w", unlocked_path, openErr)
			continue
		}
		defer db.Close()

		rows, queryErr := db.Query(query)
		if queryErr != nil {
			log.Printf("Failed to query database %s: %v. Trying next path.", unlocked_path, queryErr)
			lastErr = fmt.Errorf("failed to query database %s: %w", unlocked_path, queryErr)
			continue
		}

		var url string
		scanSuccessful := false
		for rows.Next() {
			if scanErr := rows.Scan(&url); scanErr != nil {
				rows.Close() // Close rows before returning/continuing on scan error
				log.Printf("Failed to scan row from %s: %v. Trying next path.", unlocked_path, scanErr)
				lastErr = fmt.Errorf("failed to scan row from %s: %w", unlocked_path, scanErr)
				goto nextPath // Use goto to break outer loop and ensure db/rows are closed
			}
			urls = append(urls, url)
			scanSuccessful = true
		}
		rows.Close() // Explicitly close rows

		if scanSuccessful { // If we successfully processed one history file, return
			log.Printf("Successfully extracted %d URLs from %s", len(urls), path)
			return urls, nil
		}
		// If no URLs were found in this valid file, it might be empty or all filtered out.
		// Continue to try other paths if available.
		log.Printf("No URLs extracted from %s, trying next path if available.", path)

		nextPath: // Label for goto
	}

	if len(urls) > 0 {
		// This case would be hit if the last successfully scanned DB had no URLs, but a previous one did.
		return urls, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("could not successfully process any Chrome history file: %w", lastErr)
	}
	return nil, fmt.Errorf("no Chrome history found or accessible at expected paths")
}

// Hostname returns the hostname portion of a URL
func Hostname(rawurl string) (host string, err error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL %s: %w", rawurl, err)
	}
	return u.Host, nil
}

// ExternalHostnames filters a list of hostnames to only include external ones.
func ExternalHostnames(records []string) (output []string) {
	for _, record := range records {
		host, err := Hostname(record)
		if err != nil {
			continue
		}
		// TODO(tstromberg): This should be PublicSuffix(host) != host
		// TODO(tstromberg): This needs to be an actual TLD, not just the suffix.
		suffix, err := psl.EffectiveTLDPlusOne(host)
		if err != nil || suffix == host {
			continue
		}
		output = append(output, host)
	}
	return
}

// Uniq filters a list of strings to only include unique values.
func Uniq(records []string) (output []string) {
	present := map[string]bool{}
	for _, record := range records {
		if !present[record] {
			present[record] = true
			output = append(output, record)
		}
	}
	return
}

// Random returns X random records from a list of strings.
func Random(count int, records []string) (output []string) {
	if count <= 0 {
		return []string{}
	}
	if count > len(records) {
		count = len(records)
	}
	// TODO(tstromberg): This is not a good way to pick random records.
	// The list should be shuffled, then the first X records picked.
	for i := 0; i < count; i++ {
		output = append(output, records[rand.Intn(len(records))])
	}
	return
}
