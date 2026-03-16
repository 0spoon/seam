// Package capture provides URL fetching and voice transcription capabilities,
// creating notes from external content.
package capture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// Domain errors.
var (
	ErrInvalidURL   = errors.New("invalid URL")
	ErrFetchFailed  = errors.New("URL fetch failed")
	ErrPrivateIP    = errors.New("URL points to private/loopback address")
	ErrUnsafeScheme = errors.New("URL scheme not allowed")
)

// Version is the application version included in the User-Agent header.
// Set by the server at startup; defaults to "dev" if unset.
var Version = "dev"

// URLFetcher fetches and extracts content from URLs.
type URLFetcher struct {
	client *http.Client
}

// allowedRedirectSchemes are the only URL schemes allowed in redirect targets.
var allowedRedirectSchemes = map[string]bool{
	"http":  true,
	"https": true,
}

// NewURLFetcher creates a new URLFetcher with SSRF protections.
func NewURLFetcher() *URLFetcher {
	transport := &http.Transport{
		DialContext: ssrfSafeDialer,
	}
	return &URLFetcher{
		client: &http.Client{
			Timeout:   15 * time.Second,
			Transport: transport,
			// B-8: Follow redirects but limit to 10 and validate redirect scheme.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				// Reject redirects to non-HTTP(S) schemes (e.g., file://, ftp://).
				scheme := strings.ToLower(req.URL.Scheme)
				if !allowedRedirectSchemes[scheme] {
					return fmt.Errorf("%w: redirect to %s", ErrUnsafeScheme, scheme)
				}
				return nil
			},
		},
	}
}

// ssrfSafeDialer rejects connections to private/loopback addresses.
// A-3: Connects to the validated IP directly to prevent DNS rebinding (TOCTOU).
func ssrfSafeDialer(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("capture.ssrfSafeDialer: %w", err)
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("capture.ssrfSafeDialer: resolve: %w", err)
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("capture.ssrfSafeDialer: no addresses found for %s", host)
	}

	for _, ip := range ips {
		if isPrivateIP(ip.IP) {
			return nil, ErrPrivateIP
		}
	}

	// A-3: Connect directly to the validated IP address instead of
	// re-resolving the hostname, preventing DNS rebinding attacks.
	validatedAddr := net.JoinHostPort(ips[0].IP.String(), port)
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, validatedAddr)
}

// isPrivateIP checks if an IP is private, loopback, link-local, or unspecified.
// A-4: Added IsUnspecified() to block 0.0.0.0 which routes to localhost on many OSes.
func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

// URLContent holds the extracted content from a fetched URL.
type URLContent struct {
	Title string
	Body  string
	URL   string
}

// FetchURL fetches the given URL and extracts the title and main content.
func (f *URLFetcher) FetchURL(ctx context.Context, rawURL string) (*URLContent, error) {
	// Validate URL.
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("capture.FetchURL: %w: %w", ErrInvalidURL, err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("capture.FetchURL: %w: %s", ErrUnsafeScheme, scheme)
	}

	if parsed.Host == "" {
		return nil, fmt.Errorf("capture.FetchURL: %w: empty host", ErrInvalidURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("capture.FetchURL: %w", err)
	}
	req.Header.Set("User-Agent", "Seam/"+Version+" (knowledge-system)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("capture.FetchURL: %w: %w", ErrFetchFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("capture.FetchURL: %w: status %d", ErrFetchFailed, resp.StatusCode)
	}

	// Limit read to 2MB to prevent memory issues.
	limited := io.LimitReader(resp.Body, 2*1024*1024)

	// Detect charset from Content-Type header and convert to UTF-8 if needed.
	contentType := resp.Header.Get("Content-Type")
	utf8Reader, err := charset.NewReader(limited, contentType)
	if err != nil {
		// Fallback to raw reader if charset detection fails.
		utf8Reader = limited
	}

	doc, err := html.Parse(utf8Reader)
	if err != nil {
		return nil, fmt.Errorf("capture.FetchURL: parse html: %w", err)
	}

	title := extractTitle(doc)
	if title == "" {
		title = extractOGTitle(doc)
	}
	body := extractMainContent(doc)

	if title == "" {
		// Derive title from URL if page has no title.
		title = parsed.Host + parsed.Path
	}

	return &URLContent{
		Title: title,
		Body:  body,
		URL:   rawURL,
	}, nil
}

// extractOGTitle looks for <meta property="og:title" content="..."> in the document.
func extractOGTitle(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "meta" {
		var property, content string
		for _, attr := range n.Attr {
			switch attr.Key {
			case "property":
				property = attr.Val
			case "content":
				content = attr.Val
			}
		}
		if property == "og:title" && content != "" {
			return strings.TrimSpace(content)
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if t := extractOGTitle(c); t != "" {
			return t
		}
	}
	return ""
}

// extractTitle finds the <title> element text.
func extractTitle(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "title" {
		return collectText(n)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if t := extractTitle(c); t != "" {
			return strings.TrimSpace(t)
		}
	}
	return ""
}

// extractMainContent attempts to extract the main readable content.
// It looks for <article>, <main>, then falls back to <body>.
func extractMainContent(doc *html.Node) string {
	// Priority: <article> > <main> > <body>
	for _, tag := range []string{"article", "main", "body"} {
		if node := findElement(doc, tag); node != nil {
			text := collectText(node)
			text = cleanText(text)
			if text != "" {
				return text
			}
		}
	}
	return ""
}

// findElement finds the first element with the given tag name, depth-first.
func findElement(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findElement(c, tag); found != nil {
			return found
		}
	}
	return nil
}

// collectText recursively collects text from a node, skipping script/style.
func collectText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	if n.Type == html.ElementNode {
		switch n.Data {
		case "script", "style", "noscript", "nav", "footer", "header":
			return ""
		case "br":
			return "\n"
		case "p", "div", "section", "article", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6":
			var sb strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				sb.WriteString(collectText(c))
			}
			return sb.String() + "\n\n"
		}
	}

	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(collectText(c))
	}
	return sb.String()
}

// cleanText normalizes whitespace: collapses multiple blank lines, trims.
func cleanText(s string) string {
	// Split into lines and clean each one.
	lines := strings.Split(s, "\n")
	var cleaned []string
	prevBlank := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if !prevBlank {
				cleaned = append(cleaned, "")
				prevBlank = true
			}
			continue
		}
		prevBlank = false
		cleaned = append(cleaned, line)
	}

	result := strings.Join(cleaned, "\n")
	return strings.TrimSpace(result)
}
