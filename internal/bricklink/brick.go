package bricklink

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"go.uber.org/zap"
	"golang.org/x/text/language"
)

// ErrBrickNotFound is returned when a brick is not found on BrickLink
var ErrBrickNotFound = errors.New("brick not found on BrickLink")

// Brick represents brick details fetched from BrickLink
type Brick struct {
	ItemNo          string `json:"item_no"`
	AlternateItemNo string `json:"alternate_item_no"`
	Name            string `json:"name"`
	ImageURL        string `json:"image_url"`
}

// FetchBrickDetails fetches brick details from BrickLink catalog page
func (c *Client) FetchBrickDetails(itemID string, lang language.Tag) (*Brick, error) {
	// Build the URL to the catalog page
	requestURL := fmt.Sprintf("https://www.bricklink.com/v2/catalog/catalogitem.page?P=%s", itemID)

	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers similar to the example provided
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", lang.String()+",en;q=0.9")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://www.bricklink.com/")
	req.Header.Set("sec-fetch-dest", "document")
	req.Header.Set("sec-fetch-mode", "navigate")
	req.Header.Set("sec-fetch-site", "none")
	req.Header.Set("sec-fetch-user", "?1")
	req.Header.Set("upgrade-insecure-requests", "1")

	zap.L().Info("Fetching BrickLink brick details",
		zap.String("item_id", itemID),
		zap.String("url", requestURL))

	// Execute the request with rate limiting and retry
	resp, err := c.throttler.DoWithRetry(req.Context(), c.httpClient, req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch data: %w", err)
	}
	defer resp.Body.Close()

	// Log rate limit headers if present
	c.throttler.LogRateLimitHeaders(resp)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse the HTML to extract brick details
	brick, err := parseBrickDetails(string(body), itemID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse brick details: %w", err)
	}

	return brick, nil
}

// parseBrickDetails extracts brick information from the HTML response
func parseBrickDetails(html, itemID string) (*Brick, error) {
	// Check if this is a "no results found" page (search results page instead of catalog page)
	// This happens when the item doesn't exist or the URL redirects to a search

	// Multiple indicators that the item was not found:

	// 1. "Page Not Found" title (classic error page)
	if strings.Contains(html, `<title>BrickLink Page Not Found</title>`) {
		return nil, fmt.Errorf("%w: BrickLink returned 'Page Not Found' for ID %s", ErrBrickNotFound, itemID)
	}

	// 2. "No Item(s) were found" message (classic error page)
	if strings.Contains(html, `No Item(s) were found`) || strings.Contains(html, `No Item(s) were found.  Please try again!`) {
		return nil, fmt.Errorf("%w: no items found for ID %s", ErrBrickNotFound, itemID)
	}

	// 3. Page title indicates search results (search page format)
	if strings.Contains(html, `<title>Search result for`) || strings.Contains(html, `BrickLink Search | BrickLink</title>`) {
		return nil, fmt.Errorf("%w: no matching items for ID %s", ErrBrickNotFound, itemID)
	}

	// 4. "All Item Search: Results for" text in the page
	if strings.Contains(html, `All Item Search: Results for`) {
		return nil, fmt.Errorf("%w: BrickLink returned search results instead of catalog page for ID %s", ErrBrickNotFound, itemID)
	}

	// 5. "idNoResults" element present
	if strings.Contains(html, `id="idNoResults"`) {
		return nil, fmt.Errorf("%w: no matching items for ID %s", ErrBrickNotFound, itemID)
	}

	// 6. "We couldn't match" error message
	if strings.Contains(html, "We couldn't match") || strings.Contains(html, "Uh oh!") {
		return nil, fmt.Errorf("%w: no matching items for ID %s", ErrBrickNotFound, itemID)
	}

	brick := &Brick{
		ItemNo: itemID,
	}

	// Extract item name from h1 tag with id="item-name-title"
	nameRegex := regexp.MustCompile(`<h1[^>]*id="item-name-title"[^>]*>([^<]+)</h1>`)
	if matches := nameRegex.FindStringSubmatch(html); len(matches) > 1 {
		brick.Name = strings.TrimSpace(matches[1])
	}

	// Extract alternate item number
	// Looking for: Alternate Item No: <span style="color: #2C6EA5; font-weight: bold;">55709</span>
	altNoRegex := regexp.MustCompile(`Alternate Item No:\s*<span[^>]*>([^<]+)</span>`)
	if matches := altNoRegex.FindStringSubmatch(html); len(matches) > 1 {
		brick.AlternateItemNo = strings.TrimSpace(matches[1])
	}

	// Extract image URL from the main image
	// Looking for: strMainLImgUrl: '//img.bricklink.com/ItemImage/PN/66/32199.png'
	imgRegex := regexp.MustCompile(`strMainLImgUrl:\s*'([^']+)'`)
	if matches := imgRegex.FindStringSubmatch(html); len(matches) > 1 {
		imageURL := matches[1]
		// Add https: prefix if missing
		if strings.HasPrefix(imageURL, "//") {
			imageURL = "https:" + imageURL
		}
		brick.ImageURL = imageURL
	}

	// Validate that we got at least the name
	if brick.Name == "" {
		return nil, fmt.Errorf("failed to extract brick name from HTML")
	}

	return brick, nil
}
