package capture

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
	"golang.org/x/text/encoding/charmap"
)

func TestFetchURL_ExtractsTitle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Test Page Title</title></head><body><p>Hello world</p></body></html>`))
	}))
	defer srv.Close()

	fetcher := &URLFetcher{client: srv.Client()}
	// Override the client to use the test server's client (bypasses SSRF checks).
	fetcher.client = srv.Client()

	content, err := fetcher.FetchURL(context.Background(), srv.URL)
	require.NoError(t, err)
	require.Equal(t, "Test Page Title", content.Title)
	require.Contains(t, content.Body, "Hello world")
	require.Equal(t, srv.URL, content.URL)
}

func TestFetchURL_ExtractsArticle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html>
<head><title>Blog Post</title></head>
<body>
<nav>Navigation Menu</nav>
<article>
<h1>Important Article</h1>
<p>This is the main content of the article.</p>
<p>Second paragraph with more details.</p>
</article>
<footer>Footer content</footer>
</body></html>`))
	}))
	defer srv.Close()

	fetcher := &URLFetcher{client: srv.Client()}

	content, err := fetcher.FetchURL(context.Background(), srv.URL)
	require.NoError(t, err)
	require.Equal(t, "Blog Post", content.Title)
	require.Contains(t, content.Body, "Important Article")
	require.Contains(t, content.Body, "main content")
	// Article extraction should skip nav/footer.
	require.NotContains(t, content.Body, "Navigation Menu")
	require.NotContains(t, content.Body, "Footer content")
}

func TestFetchURL_SkipsScriptAndStyle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html>
<head>
<title>Clean Page</title>
<style>.hidden { display: none; }</style>
</head>
<body>
<script>var secret = "should not appear";</script>
<p>Visible content only</p>
</body></html>`))
	}))
	defer srv.Close()

	fetcher := &URLFetcher{client: srv.Client()}

	content, err := fetcher.FetchURL(context.Background(), srv.URL)
	require.NoError(t, err)
	require.Contains(t, content.Body, "Visible content only")
	require.NotContains(t, content.Body, "should not appear")
	require.NotContains(t, content.Body, "display: none")
}

func TestFetchURL_InvalidScheme(t *testing.T) {
	fetcher := NewURLFetcher()

	_, err := fetcher.FetchURL(context.Background(), "file:///etc/passwd")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrUnsafeScheme)
}

func TestFetchURL_EmptyHost(t *testing.T) {
	fetcher := NewURLFetcher()

	_, err := fetcher.FetchURL(context.Background(), "http://")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidURL)
}

func TestFetchURL_NonHTTPStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	fetcher := &URLFetcher{client: srv.Client()}

	_, err := fetcher.FetchURL(context.Background(), srv.URL)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrFetchFailed)
}

func TestFetchURL_NoTitle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body><p>No title here</p></body></html>`))
	}))
	defer srv.Close()

	fetcher := &URLFetcher{client: srv.Client()}

	content, err := fetcher.FetchURL(context.Background(), srv.URL)
	require.NoError(t, err)
	// Title should be derived from URL when absent.
	require.NotEmpty(t, content.Title)
	require.Contains(t, content.Body, "No title here")
}

func TestCleanText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "collapses blank lines",
			input:    "line1\n\n\n\nline2",
			expected: "line1\n\nline2",
		},
		{
			name:     "trims whitespace",
			input:    "  hello  \n  world  ",
			expected: "hello\nworld",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanText(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		private bool
	}{
		{"loopback v4", "127.0.0.1", true},
		{"loopback v6", "::1", true},
		{"private 10.x", "10.0.0.1", true},
		{"private 192.168.x", "192.168.1.1", true},
		{"private 172.16.x", "172.16.0.1", true},
		{"public", "8.8.8.8", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.input)
			require.NotNil(t, ip, "failed to parse IP: %s", tt.input)
			require.Equal(t, tt.private, isPrivateIP(ip))
		})
	}
}

func TestFetchURL_ExtractTitleFromOG(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html>
<head>
<meta property="og:title" content="OG Page Title">
</head>
<body><p>Content here</p></body>
</html>`))
	}))
	defer srv.Close()

	fetcher := &URLFetcher{client: srv.Client()}

	content, err := fetcher.FetchURL(context.Background(), srv.URL)
	require.NoError(t, err)
	require.Equal(t, "OG Page Title", content.Title)
}

func TestExtractOGTitle(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected string
	}{
		{
			name:     "finds og:title meta tag",
			html:     `<html><head><meta property="og:title" content="My OG Title"></head><body></body></html>`,
			expected: "My OG Title",
		},
		{
			name:     "returns empty when no og:title",
			html:     `<html><head><meta property="og:description" content="desc"></head><body></body></html>`,
			expected: "",
		},
		{
			name:     "returns empty for empty content",
			html:     `<html><head><meta property="og:title" content=""></head><body></body></html>`,
			expected: "",
		},
		{
			name:     "trims whitespace from og:title",
			html:     `<html><head><meta property="og:title" content="  Padded Title  "></head><body></body></html>`,
			expected: "Padded Title",
		},
		{
			name:     "no meta tags at all",
			html:     `<html><head></head><body></body></html>`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := html.Parse(strings.NewReader(tt.html))
			require.NoError(t, err)
			result := extractOGTitle(doc)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestFetchURL_StripHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html>
<head><title>Strip Test</title></head>
<body>
<p>This is <strong>bold</strong> and <em>italic</em> and <a href="http://example.com">a link</a> text.</p>
</body></html>`))
	}))
	defer srv.Close()

	fetcher := &URLFetcher{client: srv.Client()}

	content, err := fetcher.FetchURL(context.Background(), srv.URL)
	require.NoError(t, err)
	require.Contains(t, content.Body, "This is bold and italic and a link text.")
	require.NotContains(t, content.Body, "<strong>")
	require.NotContains(t, content.Body, "<em>")
	require.NotContains(t, content.Body, "<a ")
	require.NotContains(t, content.Body, "</a>")
}

func TestFetchURL_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the request context is cancelled.
		<-r.Context().Done()
	}))
	defer srv.Close()

	fetcher := &URLFetcher{client: srv.Client()}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := fetcher.FetchURL(ctx, srv.URL)
	require.Error(t, err)
	require.True(t,
		strings.Contains(err.Error(), "context deadline exceeded") ||
			strings.Contains(err.Error(), "context canceled"),
		"expected timeout/cancel error, got: %v", err)
}

func TestFetchURL_LargePage(t *testing.T) {
	// Create a page larger than 2MB.
	largeBody := strings.Repeat("x", 3*1024*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><head><title>Large Page</title></head><body><p>%s</p></body></html>`, largeBody)
	}))
	defer srv.Close()

	fetcher := &URLFetcher{client: srv.Client()}

	content, err := fetcher.FetchURL(context.Background(), srv.URL)
	require.NoError(t, err)
	require.Equal(t, "Large Page", content.Title)
	// Body should be present but truncated (less than the full 3MB).
	require.NotEmpty(t, content.Body)
	require.Less(t, len(content.Body), 3*1024*1024)
}

func TestFetchURL_EncodingUTF8(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html>
<head><title>UTF-8 Test</title></head>
<body><p>Hello world.</p></body></html>`))
	}))
	defer srv.Close()

	fetcher := &URLFetcher{client: srv.Client()}

	content, err := fetcher.FetchURL(context.Background(), srv.URL)
	require.NoError(t, err)
	require.Equal(t, "UTF-8 Test", content.Title)
	require.Contains(t, content.Body, "Hello world.")
}

func TestFetchURL_EncodingLatin1(t *testing.T) {
	// Encode Latin-1 specific characters: e-acute, u-umlaut, n-tilde.
	encoder := charmap.ISO8859_1.NewEncoder()
	latin1Title, err := encoder.Bytes([]byte("Caf\u00e9"))
	require.NoError(t, err)
	latin1Body, err := encoder.Bytes([]byte("Cr\u00e8me br\u00fbl\u00e9e"))
	require.NoError(t, err)

	var page bytes.Buffer
	page.WriteString(`<html><head><title>`)
	page.Write(latin1Title)
	page.WriteString(`</title></head><body><p>`)
	page.Write(latin1Body)
	page.WriteString(`</p></body></html>`)

	pageBytes := page.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=iso-8859-1")
		w.Write(pageBytes)
	}))
	defer srv.Close()

	fetcher := &URLFetcher{client: srv.Client()}

	content, err := fetcher.FetchURL(context.Background(), srv.URL)
	require.NoError(t, err)
	require.Equal(t, "Caf\u00e9", content.Title)
	require.Contains(t, content.Body, "Cr\u00e8me br\u00fbl\u00e9e")
}

func TestFetchURL_RedirectFollowed(t *testing.T) {
	// Destination server with the actual content.
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Final Destination</title></head><body><p>Redirected content</p></body></html>`))
	}))
	defer dest.Close()

	// Origin server that redirects to the destination.
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dest.URL, http.StatusFound)
	}))
	defer origin.Close()

	// Use a plain client that trusts both test servers and follows redirects.
	fetcher := &URLFetcher{client: &http.Client{}}

	content, err := fetcher.FetchURL(context.Background(), origin.URL)
	require.NoError(t, err)
	require.Equal(t, "Final Destination", content.Title)
	require.Contains(t, content.Body, "Redirected content")
}

func TestFetchURL_PrivateIP_Handler(t *testing.T) {
	// Test isPrivateIP with additional private/link-local addresses.
	tests := []struct {
		name    string
		input   string
		private bool
	}{
		{"link-local v4", "169.254.169.254", true},
		{"link-local v4 other", "169.254.1.1", true},
		{"link-local v6", "fe80::1", true},
		{"private 10.255.255.255", "10.255.255.255", true},
		{"private 172.31.255.255", "172.31.255.255", true},
		{"public cloudflare", "1.1.1.1", false},
		{"public google", "8.8.4.4", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.input)
			require.NotNil(t, ip, "failed to parse IP: %s", tt.input)
			require.Equal(t, tt.private, isPrivateIP(ip))
		})
	}
}

func TestSSRF_MetadataEndpoint(t *testing.T) {
	// Cloud metadata endpoint 169.254.169.254 is link-local and must be blocked.
	ip := net.ParseIP("169.254.169.254")
	require.NotNil(t, ip)
	require.True(t, isPrivateIP(ip), "metadata endpoint 169.254.169.254 should be blocked")
}

func TestSSRF_IPv6Loopback(t *testing.T) {
	ip := net.ParseIP("::1")
	require.NotNil(t, ip)
	require.True(t, isPrivateIP(ip), "IPv6 loopback ::1 should be blocked")
}

func TestSSRF_UnsafeSchemes(t *testing.T) {
	// file:// is already covered by TestFetchURL_InvalidScheme.
	// Test additional dangerous schemes.
	fetcher := NewURLFetcher()

	tests := []struct {
		name string
		url  string
	}{
		{"ftp scheme", "ftp://example.com/file.txt"},
		{"javascript scheme", "javascript:alert(1)"},
		{"data scheme", "data:text/html,<h1>hi</h1>"},
		{"gopher scheme", "gopher://evil.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := fetcher.FetchURL(context.Background(), tt.url)
			require.Error(t, err)
			require.ErrorIs(t, err, ErrUnsafeScheme)
		})
	}
}

func TestSSRF_PrivateRanges_Comprehensive(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		private bool
	}{
		// Private/blocked ranges.
		{"loopback v4", "127.0.0.1", true},
		{"loopback v6", "::1", true},
		{"private class A", "10.0.0.1", true},
		{"private class B start", "172.16.0.1", true},
		{"private class B end", "172.31.255.255", true},
		{"private class C", "192.168.1.1", true},
		{"link-local metadata", "169.254.169.254", true},
		{"IPv6 link-local", "fe80::1", true},
		{"private class A upper", "10.255.255.255", true},
		{"loopback v4 high", "127.255.255.254", true},

		// Public ranges that must NOT be blocked.
		{"public Google DNS", "8.8.8.8", false},
		{"public Cloudflare DNS", "1.1.1.1", false},
		{"public documentation range", "203.0.113.1", false},
		{"just below private class B", "172.15.255.255", false},
		{"just above private class B", "172.32.0.1", false},
		{"public IPv6", "2001:4860:4860::8888", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.input)
			require.NotNil(t, ip, "failed to parse IP: %s", tt.input)
			require.Equal(t, tt.private, isPrivateIP(ip),
				"isPrivateIP(%s) = %v, want %v", tt.input, !tt.private, tt.private)
		})
	}
}

func TestFetchURL_OGTitlePreference(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html>
<head>
<title>HTML Title</title>
<meta property="og:title" content="OG Title">
</head>
<body><p>Body content</p></body>
</html>`))
	}))
	defer srv.Close()

	fetcher := &URLFetcher{client: srv.Client()}

	content, err := fetcher.FetchURL(context.Background(), srv.URL)
	require.NoError(t, err)
	// <title> should be preferred over og:title.
	require.Equal(t, "HTML Title", content.Title)
}
