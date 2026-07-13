package rebrickable

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"go.uber.org/zap"
)

// ErrPartNotFound indicates that the part (or part+color combination) was not found.
var ErrPartNotFound = errors.New("part not found in rebrickable")

// Part holds the catalog details of a LEGO part as provided by Rebrickable.
type Part struct {
	PartNum    string `json:"part_num"`
	Name       string `json:"name"`
	PartImgURL string `json:"part_img_url"`
	// Molds lists other part numbers that are physical mold variations of this part
	// (the closest equivalent to BrickLink's "alternate item numbers").
	Molds []string `json:"molds"`
	// Alternates lists functionally equivalent part numbers.
	Alternates []string `json:"alternates"`
	// Prints lists printed variations of this part.
	Prints []string `json:"prints"`
}

// partColorResponse is the payload of GET /lego/parts/{part_num}/colors/{color_id}/.
type partColorResponse struct {
	PartImgURL string   `json:"part_img_url"`
	Elements   []string `json:"elements"`
}

// FetchPartDetails fetches the catalog details of a part
// (GET /lego/parts/{part_num}/).
func (c *Client) FetchPartDetails(partNum string) (*Part, error) {
	if c.apiKey == "" {
		return nil, ErrMissingAPIKey
	}

	requestURL := fmt.Sprintf("%s/lego/parts/%s/", c.apiBaseURL, url.PathEscape(partNum))

	body, status, err := c.getJSON(requestURL)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, ErrPartNotFound
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d: %s", status, string(body))
	}

	var part Part
	if err := json.Unmarshal(body, &part); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	zap.L().Debug("Fetched Rebrickable part details",
		zap.String("part_num", partNum),
		zap.String("name", part.Name))

	return &part, nil
}

// FetchPartColorElements returns every LEGO element ID known for a part in a given
// color (GET /lego/parts/{part_num}/colors/{color_id}/). This is the fallback used
// when the single element ID carried by a set inventory line is not purchasable on
// Pick-a-Brick: sibling element IDs of the same part+color often are.
func (c *Client) FetchPartColorElements(partNum string, colorID int) ([]string, error) {
	if c.apiKey == "" {
		return nil, ErrMissingAPIKey
	}

	requestURL := fmt.Sprintf("%s/lego/parts/%s/colors/%d/",
		c.apiBaseURL, url.PathEscape(partNum), colorID)

	body, status, err := c.getJSON(requestURL)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, ErrPartNotFound
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d: %s", status, string(body))
	}

	var resp partColorResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	zap.L().Debug("Fetched Rebrickable part color elements",
		zap.String("part_num", partNum),
		zap.Int("color_id", colorID),
		zap.Int("elements", len(resp.Elements)))

	return resp.Elements, nil
}

// getJSON performs a single authenticated GET and returns the raw body and status.
func (c *Client) getJSON(requestURL string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "key "+c.apiKey)

	resp, err := c.throttler.DoWithRetry(req.Context(), c.httpClient, req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch data: %w", err)
	}
	defer resp.Body.Close()

	c.throttler.LogRateLimitHeaders(resp)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}
	return body, resp.StatusCode, nil
}
