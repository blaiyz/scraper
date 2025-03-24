package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/net/html"
)

type ScrapeData struct {
	base      *url.URL
	url       *url.URL
	client    *http.Client
	deadlinks chan<- *url.URL
	nextlinks chan<- *url.URL
	wg        *sync.WaitGroup
}

type WorkerData struct {
	base      *url.URL
	client    *http.Client
	deadlinks chan<- *url.URL
	nextlinks chan<- *url.URL
	jobs      <-chan *url.URL
	wg        *sync.WaitGroup
}

const (
	Timeout    = 5
	ChannelCap = 100
)

func StartScraper(targetUrl string, workersCount int) ([]string, error) {
	parsedTargetUrl, err := cleanURL(targetUrl, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout: Timeout * time.Second,
	}

	var wg sync.WaitGroup
	deadlinks := make(chan *url.URL, ChannelCap)
	allDeadlinks := make([]string, 0)
	nextlinks := make(chan *url.URL, ChannelCap)
	jobs := make(chan *url.URL, ChannelCap)
	visitedLinks := make(map[string]struct{}, ChannelCap)
	ctx := context.Background()

	// Start workers
	data := &WorkerData{
		base:      parsedTargetUrl,
		client:    client,
		deadlinks: deadlinks,
		nextlinks: nextlinks,
		jobs:      jobs,
		wg:        &wg,
	}
	for range workersCount {
		go worker(data, ctx)
	}

	// Start new link handler
	go func() {
		for nextlink := range nextlinks {
			slog.Debug(fmt.Sprintf("Processing %s", nextlink))
			if _, exists := visitedLinks[nextlink.String()]; exists {
				wg.Done()
				continue
			}
			visitedLinks[nextlink.String()] = struct{}{}
			jobs <- nextlink
		}
	}()

	// Start deadlink slice updater
	var deadlinkWg sync.WaitGroup
	deadlinkWg.Add(1)
	go func() {
		for deadlink := range deadlinks {
			allDeadlinks = append(allDeadlinks, deadlink.String())
		}
		deadlinkWg.Done()
	}()

	// Add first job
	wg.Add(1)
	nextlinks <- parsedTargetUrl

	wg.Wait()

	slog.Info("Done scraping, closing channels")
	close(nextlinks)
	close(jobs)
	close(deadlinks)
	deadlinkWg.Wait()

	slog.Debug("Returning")
	return allDeadlinks, nil
}

func worker(data *WorkerData, ctx context.Context) {
	for nextlink := range data.jobs {
		scrapeData := ScrapeData{
			base:      data.base,
			url:       nextlink,
			client:    data.client,
			deadlinks: data.deadlinks,
			nextlinks: data.nextlinks,
			wg:        data.wg,
		}
		scrapePage(&scrapeData, ctx)
		data.wg.Done()
	}
}

func scrapePage(data *ScrapeData, ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, data.url.String(), nil)
	if err != nil {
		slog.Warn("Could not create request")
		return
	}

	slog.Info(fmt.Sprintf("Sending request to %s", data.url.String()))
	resp, err := data.client.Do(req)
	if err != nil {
		// Check if the context was canceled or deadline was exceeded
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			slog.Info(fmt.Sprintf("Request canceled or timed out: %s", data.url))
			return
		}
		slog.Info(fmt.Sprintf("Found dead link: %s, error: %s", data.url, err.Error()))
		data.deadlinks <- data.url
		return
	}
	defer resp.Body.Close()
	slog.Debug(fmt.Sprintf("Request success %s", data.url))

	// Check if this is a dead link
	if resp.StatusCode >= 400 && resp.StatusCode <= 599 {
		slog.Info(fmt.Sprintf("Found deadlink: %s, resp: %+v", data.url, resp))
		data.deadlinks <- data.url
		return
	}

	// From this point, this url is not a deadlink.
	// We will now extract all links in this page and send
	// them to be checked.

	// Stop scraping outside target website
	if !isSameDomain(data.url, data.base) {
		slog.Info(fmt.Sprintf("Avoiding leaving domain: %s", data.url))
		return
	}

	links, err := extractLinks(resp.Body, data.base)
	if err != nil {
		slog.Error(fmt.Sprintf("Error extracting links from %s: %s", data.url, err.Error()))
		return
	}

	data.wg.Add(len(links))
	for _, link := range links {
		data.nextlinks <- link
	}
}

func extractLinks(respBody io.Reader, base *url.URL) ([]*url.URL, error) {
	doc, err := html.Parse(respBody)
	if err != nil {
		slog.Error("Could not parse body")
		return nil, err
	}

	links := make([]*url.URL, 0)
	var traverse func(*html.Node)
	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					link := attr.Val
					clean, err2 := cleanURL(link, base)
					if err2 != nil {
						slog.Error(fmt.Sprintf("Failed to clean URL: %s", err2.Error()))
						continue
					}
					links = append(links, clean)
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			traverse(child)
		}
	}
	traverse(doc)
	return links, nil
}

func isSameDomain(url1 *url.URL, url2 *url.URL) bool {
	return url1.Host == url2.Host
}

func cleanURL(href string, base *url.URL) (*url.URL, error) {
	u, err := url.Parse(href)
	if err != nil {
		return &url.URL{}, err
	}
	// Clear query parameters and fragment
	u.RawQuery = ""
	u.Fragment = ""

	if u.IsAbs() {
		return u, nil
	}

	if base == nil {
		return &url.URL{}, errors.New("cleanURL: cannot parse a non absolute url without a base")
	}

	return base.ResolveReference(u), nil
}
