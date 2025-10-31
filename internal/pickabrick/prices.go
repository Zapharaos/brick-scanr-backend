package pickabrick

import (
	"fmt"
	"net/http"
	"net/url"

	"go.uber.org/zap"
)

type Brick struct {
	ItemID string  `json:"item_id" example:"123456"`
	ItemNo string  `json:"item_no" example:"3001"`
	Price  float64 `json:"price,omitempty" example:"0.15"`
}

// FetchBrickPrice fetches the price for a specific LEGO piece from Pick-a-Brick
// TODO: Implement actual scraping logic for LEGO Pick-a-Brick prices
func (c *Client) FetchBrickPrice(itemNo string) (float64, error) {
	// Placeholder implementation
	// The actual implementation should:
	// 1. Build the correct Pick-a-Brick URL for the item
	// 2. Scrape the page to find the price
	// 3. Parse and return the price as a float64

	zap.L().Debug("Fetching Pick-a-Brick price",
		zap.String("item_no", itemNo),
	)

	// Example URL structure (needs to be verified):
	// https://www.lego.com/en-us/page/static/pick-a-brick
	baseURL := "https://www.lego.com/en-us/page/static/pick-a-brick"
	params := url.Values{}
	params.Add("query", itemNo)

	requestURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())

	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	// TODO: Implement actual price scraping
	// resp, err := c.httpClient.Do(req)
	// if err != nil {
	//     return 0, fmt.Errorf("failed to fetch data: %w", err)
	// }
	// defer resp.Body.Close()

	// For now, return an error indicating this is not implemented
	return 0, fmt.Errorf("price fetching not yet implemented for item %s", itemNo)
}
