package bricklink

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/mocks"
	"go.uber.org/zap"
)

// FetchSetDetails fetches detailed information for a set by its item ID
func (c *Client) FetchSetDetails(itemID int) (*Set, error) {
	itemIDStr := strconv.Itoa(itemID)

	// If mock mode is enabled, load from mock file
	if c.useMocks {
		zap.L().Info("Using mock data for BrickLink set details", zap.Int("item_id", itemID))

		data, err := mocks.LoadBricklinkSetDetailsMock(itemIDStr)
		if err != nil {
			return nil, fmt.Errorf("failed to load mock set details data: %w", err)
		}

		var setResp setDetailsResponse
		if err := mocks.UnmarshalJSON(data, &setResp); err != nil {
			return nil, err
		}

		if setResp.ReturnCode != 0 {
			return nil, fmt.Errorf("mock set details API returned error code %d: %s",
				setResp.ReturnCode, setResp.ReturnMessage)
		}

		zap.L().Info("Loaded set details from mock data", zap.Int("item_id", itemID))

		return &setResp.Item, nil
	}

	baseURL := "https://www.bricklink.com/ajax/renovate/catalog/getItemImageList.ajax"
	params := url.Values{}
	params.Add("idItem", itemIDStr)
	params.Add("idColor", "-1")      // -1 to include all colors
	params.Add("bIncludeAssoc", "0") // Include associated items

	// Add timestamp to prevent caching
	params.Add("_", strconv.FormatInt(time.Now().UnixMilli(), 10))

	requestURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())

	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://www.bricklink.com/")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	zap.L().Info("Fetching BrickLink set details",
		zap.Int("item_id", itemID),
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

	var setResp setDetailsResponse
	if err := json.Unmarshal(body, &setResp); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	if setResp.ReturnCode != 0 {
		return nil, fmt.Errorf("set details API returned error code %d: %s",
			setResp.ReturnCode, setResp.ReturnMessage)
	}

	zap.L().Info("Fetched set details from BrickLink", zap.Int("item_id", itemID))

	return &setResp.Item, nil
}
