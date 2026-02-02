package lego

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"go.uber.org/zap"
	"golang.org/x/text/language"
)

// FetchProductDetails fetches product details from LEGO's official API using the product slug
func (c *Client) FetchProductDetails(slug string, currency language.Tag) (*Product, error) {
	// TODO : fix : review x-locale vs accept-language vs referer + front URL with those

	// Build the GraphQL request URL
	baseURL := "https://www.lego.com/api/graphql/ProductDetails"

	// Prepare variables
	variables := map[string]string{
		"slug": slug,
	}
	variablesJSON, err := json.Marshal(variables)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal variables: %w", err)
	}

	// Prepare extensions with locale and persisted query
	extensions := map[string]interface{}{
		"locale": currency.String(),
		"persistedQuery": map[string]interface{}{
			"version":    1,
			"sha256Hash": "27f8e800bed4b4e47c81ab976436c641b50e3683a41412d9496e90ae79dd19da",
		},
	}
	extensionsJSON, err := json.Marshal(extensions)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal extensions: %w", err)
	}

	// Build query parameters
	params := url.Values{}
	params.Add("variables", string(variablesJSON))
	params.Add("extensions", string(extensionsJSON))

	requestURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())

	// Create HTTP request
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set required headers
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", currency.String()+",en;q=0.9")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-locale", currency.String())
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
	req.Header.Set("Referer", fmt.Sprintf("https://www.lego.com/%s/product/%s", currency.String(), slug))
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "same-origin")
	req.Header.Set("sec-fetch-site", "same-origin")

	zap.L().Info("Fetching LEGO product details",
		zap.String("slug", slug),
		zap.String("currency", currency.String()))

	// Execute the request with rate limiting and retry
	resp, err := c.throttler.DoWithRetry(req.Context(), c.httpClient, req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch data: %w", err)
	}
	defer resp.Body.Close()

	// Log rate limit headers if present
	c.throttler.LogRateLimitHeaders(resp)

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse the response
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var response ProductDetailsResponse
	if err := json.Unmarshal(bodyBytes, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	zap.L().Info("Successfully fetched LEGO product details",
		zap.String("slug", slug),
		zap.String("product_code", response.Data.Product.ProductCode),
		zap.String("product_name", response.Data.Product.Name))

	return &response.Data.Product, nil
}
