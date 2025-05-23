namebench 2.0
=============
namebench provides personalized DNS server recommendations based on your
browsing history.

WARNING: This tool is in the midst of a major rewrite. The "master" branch is currently experimental.
While it has a functional UI, it also includes new command-line interface (CLI) capabilities.

For stable binaries of the original namebench (1.x), please see https://code.google.com/p/namebench/

What can one expect in namebench 2.0?

* Faster
* Simpler interface
* More comprehensive results
* CDN benchmarking
* DNSSEC support


BUILDING:
=========
Building requires Go 1.23.0 or newer to be installed: http://golang.org/

* Clone the repository:
  ```bash
  git clone https://github.com/google/namebench.git
  cd namebench
  ```

* Build it:
  Namebench now uses Go Modules for dependency management. Dependencies will be downloaded automatically.
  ```bash
  go build .
  ```
  (Alternatively, `go build namebench.go` can also be used)

You should have an executable named 'namebench' (or `namebench.exe` on Windows) in the current directory.


RUNNING:
========
The application can be run in User Interface (UI) mode or Command-Line Interface (CLI) mode.

**UI Mode (Experimental)**
* End-user: run `./namebench`, which should open up a UI window.
  (Note: UI functionality relies on node-webkit, which needs to be available on your system.)
* Developer: `./namebench_dev_server.sh` can be used for an auto-reloading webserver at http://localhost:9080/ for UI development.

**Command-Line Interface (CLI) Mode (Experimental)**
The CLI mode allows for more direct control over the benchmarking process.

* To enable CLI mode, use the `-cli` flag.
* Key CLI flags include:
    * `-nameservers <ip1[:port1],ip2[:port2],...>`: Comma-separated list of nameservers to benchmark (e.g., `8.8.8.8,1.1.1.1:5353`). Defaults to a predefined list.
    * `-domain_source <history|default_list|filepath>`: Source for domains to test.
        * `history`: Uses domains from your Chrome browser history (macOS/Linux/Windows).
        * `default_list`: Uses a small, built-in list of popular domains.
        * `filepath`: Reads domains from the specified text file (one domain per line).
    * `-count <number>`: Number of unique domains to test (default: 20).
    * `-dnssec`: Enable DNSSEC (DO bit) in queries.
    * `-record_type <type>`: DNS record type to query (e.g., A, AAAA, MX; default: A).
* Example CLI command:
  ```bash
  ./namebench -cli -nameservers 8.8.8.8,1.1.1.1,9.9.9.9 -domain_source default_list -count 10
  ```
This will benchmark Google DNS, Cloudflare DNS, and Quad9 DNS using the default list of domains, testing 10 of them.
