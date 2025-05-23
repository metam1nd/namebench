// dnsqueue is a library for queueing up a large number of DNS requests.
package dnsqueue

import (
	"context"
	"errors"
	"fmt"
	"github.com/miekg/dns"
	"log"
	"time"
)

// Request contains data for making a DNS request
type Request struct {
	Ctx             context.Context // Context for the request
	Destination     string
	RecordType      string
	RecordName      string
	VerifySignature bool
}

// Answer contains a single answer returned by a DNS server.
type Answer struct {
	Ttl    uint32
	Name   string
	String string
}

// Result contains metadata relating to a set of DNS server results.
type Result struct {
	Request  Request
	Duration time.Duration
	Answers  []Answer
	Error    string
}

// Queue contains methods and state for setting up a request queue.
type Queue struct {
	Requests    chan *Request
	Results     chan *Result
	WorkerCount int
	Quit        chan bool // This field is unused now, can be removed if no other plans for it.
}

// StartQueue starts a new queue with max length of X with worker count Y.
func StartQueue(size, workers int) (q *Queue) {
	q = &Queue{
		Requests:    make(chan *Request, size),
		Results:     make(chan *Result, size),
		WorkerCount: workers,
	}
	for i := 0; i < q.WorkerCount; i++ {
		go startWorker(q.Requests, q.Results)
	}
	return
}

// Add adds a request to the queue. Only blocks if queue is full.
// It now accepts a context.Context and verifySignature.
func (q *Queue) Add(ctx context.Context, dest, record_type, record_name string, verifySignature bool) {
	if ctx == nil {
		log.Println("Warning: dnsqueue.Add called with nil context. Using context.Background().")
		ctx = context.Background()
	}
	req := &Request{
		Ctx:             ctx,
		Destination:     dest,
		RecordType:      record_type,
		RecordName:      record_name,
		VerifySignature: verifySignature,
	}
	q.Requests <- req
}

// SendCompletionSignal closes the Requests channel, signaling workers to complete.
func (q *Queue) SendCompletionSignal() {
	log.Printf("Closing requests channel to signal completion...")
	close(q.Requests)
}

// startWorker starts a thread to watch the request channel and populate result channel.
// It now ranges over the queue and exits when the channel is closed.
func startWorker(queue <-chan *Request, results chan<- *Result) {
	for request := range queue {
		ctxToUse := request.Ctx
		if ctxToUse == nil {
			log.Printf("Warning: Request for %s to %s had nil Ctx, using context.Background().", request.RecordName, request.Destination)
			ctxToUse = context.Background()
		}
		result, err := SendQuery(ctxToUse, request)
		if err != nil {
			// Error is already wrapped and stored in result.Error by SendQuery
			// Log that an error occurred, the details are in result.Error
			// log.Printf("Query for %s to %s resulted in error: %s", request.RecordName, request.Destination, err)
		}
		results <- &result
	}
	log.Printf("Worker finished as requests channel was closed.")
}

// SendQuery sends a DNS query via UDP, configured by a Request object and controlled by a Context.
// If successful, stores response details in Result object, otherwise, returns Result object
// with an error string.
func SendQuery(ctx context.Context, request *Request) (result Result, err error) {
	result.Request = *request

	record_type, ok := dns.StringToType[request.RecordType]
	if !ok {
		err = fmt.Errorf("invalid DNS record type %q for domain %s", request.RecordType, request.RecordName)
		result.Error = err.Error()
		return result, err
	}

	m := new(dns.Msg)
	if request.VerifySignature {
		m.SetEdns0(4096, true)
	}
	m.SetQuestion(request.RecordName, record_type)
	c := new(dns.Client)

	in, rtt, exchangeErr := c.ExchangeContext(ctx, m, request.Destination)
	result.Duration = rtt

	if exchangeErr != nil {
		err = fmt.Errorf("dns exchange failed for %s to %s (record type %s): %w", request.RecordName, request.Destination, request.RecordType, exchangeErr)
		result.Error = err.Error()
		return result, err
	}

	for _, rr := range in.Answer {
		answer := Answer{
			Ttl:    rr.Header().Ttl,
			Name:   rr.Header().Name,
			String: rr.String(),
		}
		result.Answers = append(result.Answers, answer)
	}
	return result, nil
}
