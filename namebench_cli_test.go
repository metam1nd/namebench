package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"flag"
	"fmt"
)

// TestParseNameservers tests the parseNameservers function.
func TestParseNameservers(t *testing.T) {
	defaultNS := []string{"8.8.8.8:53", "1.1.1.1:53"}

	tests := []struct {
		name     string
		nsFlag   string
		defaultN []string
		want     []string
	}{
		{
			name:     "empty input use defaults",
			nsFlag:   "",
			defaultN: defaultNS,
			want:     defaultNS,
		},
		{
			name:     "single IP no port",
			nsFlag:   "9.9.9.9",
			defaultN: defaultNS,
			want:     []string{"9.9.9.9:53"},
		},
		{
			name:     "single IP with port",
			nsFlag:   "1.2.3.4:5353",
			defaultN: defaultNS,
			want:     []string{"1.2.3.4:5353"},
		},
		{
			name:     "multiple IPs with and without ports",
			nsFlag:   "8.8.8.8,1.1.1.1:5353,208.67.222.222",
			defaultN: defaultNS,
			want:     []string{"8.8.8.8:53", "1.1.1.1:5353", "208.67.222.222:53"},
		},
		{
			name:     "comma-separated list with spaces",
			nsFlag:   "8.8.8.8 , 1.1.1.1:5353 , 4.2.2.1",
			defaultN: defaultNS,
			want:     []string{"8.8.8.8:53", "1.1.1.1:5353", "4.2.2.1:53"},
		},
		{
			name:     "empty string elements",
			nsFlag:   "8.8.8.8,,1.1.1.1",
			defaultN: defaultNS,
			want:     []string{"8.8.8.8:53", "1.1.1.1:53"},
		},
		{
			name:     "only commas",
			nsFlag:   ",,",
			defaultN: defaultNS, // Should fall back to default if all parts are empty
			want:     defaultNS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseNameservers(tt.nsFlag, tt.defaultN)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseNameservers(%q, %v) = %v, want %v", tt.nsFlag, tt.defaultN, got, tt.want)
			}
		})
	}
}

// TestLoadDomainsFromFile tests the loadDomainsFromFile function.
func TestLoadDomainsFromFile(t *testing.T) {
	// Non-existent file
	t.Run("non-existent file", func(t *testing.T) {
		_, err := loadDomainsFromFile("non_existent_file.txt")
		if err == nil {
			t.Errorf("loadDomainsFromFile with non-existent file: expected error, got nil")
		}
	})

	// Helper to create temp files
	createTempFile := func(t *testing.T, content string) string {
		t.Helper()
		tmpFile, err := ioutil.TempFile("", "test_domains_*.txt")
		if err != nil {
			t.Fatalf("Failed to create temp file: %v", err)
		}
		if _, err := tmpFile.WriteString(content); err != nil {
			tmpFile.Close()
			os.Remove(tmpFile.Name())
			t.Fatalf("Failed to write to temp file: %v", err)
		}
		tmpFile.Close()
		return tmpFile.Name()
	}

	// Empty file
	t.Run("empty file", func(t *testing.T) {
		emptyFilePath := createTempFile(t, "")
		defer os.Remove(emptyFilePath)

		domains, err := loadDomainsFromFile(emptyFilePath)
		if err != nil {
			t.Errorf("loadDomainsFromFile with empty file: expected no error, got %v", err)
		}
		if len(domains) != 0 {
			t.Errorf("loadDomainsFromFile with empty file: expected 0 domains, got %d", len(domains))
		}
	})

	// File with a few domains
	t.Run("file with domains", func(t *testing.T) {
		content := "google.com\ncloudflare.com\nexample.com"
		filePath := createTempFile(t, content)
		defer os.Remove(filePath)

		expectedDomains := []string{"google.com", "cloudflare.com", "example.com"}
		domains, err := loadDomainsFromFile(filePath)
		if err != nil {
			t.Errorf("loadDomainsFromFile with domains: expected no error, got %v", err)
		}
		if !reflect.DeepEqual(domains, expectedDomains) {
			t.Errorf("loadDomainsFromFile with domains: got %v, want %v", domains, expectedDomains)
		}
	})

	// File with domains and whitespace
	t.Run("file with domains and whitespace", func(t *testing.T) {
		content := "  google.com  \n\ncloudflare.com\n  example.com\n"
		filePath := createTempFile(t, content)
		defer os.Remove(filePath)

		expectedDomains := []string{"google.com", "cloudflare.com", "example.com"}
		domains, err := loadDomainsFromFile(filePath)
		if err != nil {
			t.Errorf("loadDomainsFromFile with domains and whitespace: expected no error, got %v", err)
		}
		if !reflect.DeepEqual(domains, expectedDomains) {
			t.Errorf("loadDomainsFromFile with domains and whitespace: got %v, want %v", domains, expectedDomains)
		}
	})
}


// FlagDefinitionCheck provides a conceptual check for flag definitions.
// It doesn't run `flag.Parse()` but verifies that the flags used in `main` are defined.
// This is more of a developer reminder as direct testing of flag definitions
// without calling Parse or examining the global `flag.CommandLine` is non-trivial.
// The actual flag variables (cli, nameservers, etc.) are not exported from main,
// so we can't directly access them here. We can, however, check if they are part of
// the default command-line flag set by trying to look them up.
// This is a limited check.
func TestFlagDefinitions(t *testing.T) {
	t.Log("Conceptual check for CLI flag definitions:")
	t.Log("This test does not execute flag.Parse() or run main().")
	t.Log("It attempts to lookup flags by name to see if they were defined by namebench.go's init or main.")

	// These are the flags defined in namebench.go using flag.String, flag.Bool, etc.
	expectedFlags := []string{
		"nw_path", "nw_package", "port", // UI flags
		"cli", "nameservers", "domain_source", "count", "record_type", "dnssec", // CLI flags
	}

	// We need to ensure that our test execution doesn't accidentally parse flags
	// meant for the main application.
	// The standard library's `flag` package registers flags on a global `CommandLine`
	// FlagSet. When `namebench.go` is compiled (even as part of a test), its `init`
	// functions (which is where `flag.String` etc. are typically called at package level)
	// will run and register these flags.
	
	// Re-initialize the flag set for this test to avoid interference from other tests or main.
	// However, this is tricky because the flags from namebench.go's init() are already registered.
	// A truly isolated test would require a custom FlagSet, but then we wouldn't be
	// checking the default CommandLine set that namebench.go's main() uses.

	// We will iterate through the flags expected to be defined by namebench.go
	var missingFlags []string
	for _, flagName := range expectedFlags {
		if fl := flag.Lookup(flagName); fl == nil {
			// This means the flag was not defined on the default flag.CommandLine
			missingFlags = append(missingFlags, flagName)
			t.Logf("Flag '%s' appears to be UNDEFINED.", flagName)
		} else {
			t.Logf("Flag '%s' is defined. Default: '%s', Usage: '%s'", fl.Name, fl.DefValue, fl.Usage)
		}
	}

	if len(missingFlags) > 0 {
		t.Errorf("The following CLI flags appear to be missing from definition: %v. "+
			"Ensure they are defined globally in namebench.go using flag.String(), flag.Bool() etc.", missingFlags)
	} else {
		t.Log("All expected CLI flags appear to be defined.")
	}
	
	// Note: This test doesn't check the *types* of the flags, only their presence by name.
	// It also doesn't check if `flag.Parse()` is called in `main()`.
	// The responsibility for `flag.Parse()` being called correctly in `main()` for the actual
	// application execution remains a manual check or an integration-style test.
}
