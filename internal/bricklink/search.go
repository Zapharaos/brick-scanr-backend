package bricklink

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"go.uber.org/zap"
)

// SearchSets searches for LEGO sets on BrickLink
func (c *Client) SearchSets(query string) ([]SearchItem, error) {
	baseURL := "https://www.bricklink.com/ajax/clone/search/searchproduct.ajax"
	params := url.Values{}
	params.Add("q", query)
	params.Add("st", "0")
	params.Add("cond", "")
	params.Add("type", "")
	params.Add("cat", "")
	params.Add("yf", "0")
	params.Add("yt", "0")
	params.Add("loc", "")
	params.Add("reg", "0")
	params.Add("ca", "0")
	params.Add("ss", "")
	params.Add("pmt", "")
	params.Add("nmp", "0")
	params.Add("color", "-1")
	params.Add("min", "0")
	params.Add("max", "0")
	params.Add("minqty", "0")
	params.Add("nosuperlot", "1")
	params.Add("incomplete", "0")
	params.Add("showempty", "1")
	params.Add("rpp", "25")
	params.Add("pi", "1")
	params.Add("ci", "0")

	requestURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())

	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://www.bricklink.com/")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	zap.L().Info("Searching LEGO sets on BrickLink", zap.String("query", query))

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

	var searchResp searchResponse
	if err := json.Unmarshal(body, &searchResp); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	if searchResp.ReturnCode != 0 {
		return nil, fmt.Errorf("search API returned error code %d: %s", searchResp.ReturnCode, searchResp.ReturnMessage)
	}

	// Filter for Sets only (type "S")
	var sets []SearchItem
	for _, typeList := range searchResp.Result.TypeList {
		if typeList.Type == "S" {
			for _, item := range typeList.Items {
				if item.TypeItem == "S" {
					sets = append(sets, item)
				}
			}
		}
	}

	zap.L().Info("Found LEGO sets on BrickLink",
		zap.String("query", query),
		zap.Int("set_count", len(sets)))

	return sets, nil
}
