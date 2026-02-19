package pickabrick

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"go.uber.org/zap"
	"golang.org/x/text/language"
)

// CDN image URL patterns from LEGO's Pick-a-Brick frontend
const (
	CDNImageSpinPhotoreal = "https://www.lego.com/cdn/product-assets/element.spin.photoreal"
	CDNImageSpinDefault   = "https://www.lego.com/cdn/product-assets/element.spin.default"
)

type AvailabilityStatus int

const (
	OutOfStock = "OUT_OF_STOCK"
	Available  = "AVAILABLE"
)

type Price struct {
	CentAmount      int     `json:"centAmount"`
	CurrencyCode    string  `json:"currencyCode"`
	FormattedAmount string  `json:"formattedAmount"`
	FormattedValue  float64 `json:"formattedValue"`
}

type ElementCategory struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

type ElementFacets struct {
	Category    *ElementCategory `json:"category,omitempty"`
	Subcategory *ElementCategory `json:"subcategory,omitempty"`
	Color       *ElementCategory `json:"color,omitempty"`
	ColorFamily *ElementCategory `json:"colorFamily,omitempty"`
	System      string           `json:"system,omitempty"`
}

type Brick struct {
	ID               string         `json:"id"`
	DesignID         string         `json:"designId"`
	CollapseDesignID string         `json:"collapseDesignId"`
	Name             string         `json:"name"`
	ImageURL         string         `json:"imageUrl"`
	ColorHex         string         `json:"colorHex"`
	ContrastColorHex string         `json:"contrastColorHex"`
	Price            Price          `json:"price"`
	Availability     string         `json:"availability"`
	MaxOrderQuantity int            `json:"maxOrderQuantity"`
	DeliveryChannel  string         `json:"deliveryChannel"`
	Facets           *ElementFacets `json:"facets,omitempty"`
}

// GetImageURL returns the image URL for this brick.
// Prioritizes constructing a CDN PNG URL using the element ID for consistency.
// Falls back to the API-provided imageUrl if element ID is not available.
// The CDN pattern is based on LEGO's Pick-a-Brick frontend code.
// Uses the spin.photoreal format which provides PNG images without fixed size constraints.
func (b *Brick) GetImageURL() string {
	// Prefer CDN URL construction using element ID for consistent PNG format
	// Format: https://www.lego.com/cdn/product-assets/element.spin.photoreal/{elementID}/00001.png
	if b.ID != "" {
		return fmt.Sprintf("%s/%s/00001.png", CDNImageSpinPhotoreal, b.ID)
	}

	// Fallback to API-provided image URL if element ID is not available
	if b.ImageURL != "" {
		return b.ImageURL
	}

	return ""
}

// GetImageURLWithFallback returns the primary image URL and a fallback URL.
// Primary: CDN PNG URL (constructed from element ID)
// Fallback: API-provided imageUrl (may be JPG)
// Use this when you want to handle potential 404s on the CDN.
func (b *Brick) GetImageURLWithFallback() (primary string, fallback string) {
	// Primary: CDN PNG URL
	if b.ID != "" {
		primary = fmt.Sprintf("%s/%s/00001.png", CDNImageSpinPhotoreal, b.ID)
	}

	// Fallback: API-provided URL
	fallback = b.ImageURL

	return primary, fallback
}

// FetchBricksByDesignID fetches all bricks matching the designID
func (c *Client) FetchBricksByDesignID(designID string, lang, xlocale language.Tag) ([]Brick, error) {

	zap.L().Debug("Fetching Pick-a-Brick elements by design ID",
		zap.String("design_id", designID),
		zap.String("lang", lang.String()),
		zap.String("xlocale", xlocale.String()),
	)

	// GraphQL query from the LEGO API
	query := `query ElementByDesignId($collapseDesignId: String!, $filters: ElementFilters, $sku: String) {
  elements(by: {collapseDesignId: $collapseDesignId}, filters: $filters) {
    ...ElementLeaf
    __typename
  }
}

fragment ElementLeaf on SearchResultElement {
  id
  designId
  collapseDesignId
  name
  imageUrl
  maxOrderQuantity
  deliveryChannel
  colorHex
  contrastColorHex
  price {
    centAmount
    formattedAmount
    currencyCode
    formattedValue
    __typename
  }
  quantityInSet(sku: $sku)
  facets {
    category {
      ...ElementFacetCategory
      __typename
    }
    subcategory {
      ...ElementFacetCategory
      __typename
    }
    color {
      ...ElementFacetCategory
      __typename
    }
    colorFamily {
      ...ElementFacetCategory
      __typename
    }
    system
    __typename
  }
  siblings {
    id
    colorHex
    contrastColorHex
    availability
    price {
      formattedAmount
      formattedValue
      __typename
    }
    __typename
  }
  availability
  __typename
}

fragment ElementFacetCategory on ElementCategory {
  name
  key
  __typename
}`

	// Build the GraphQL request
	reqBody := graphQLRequest{
		OperationName: "ElementByDesignId",
		Variables: map[string]interface{}{
			"collapseDesignId": designID,
		},
		Query: query,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	apiURL := "https://www.lego.com/api/graphql/ElementByDesignId"
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set required headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", lang.String()+",en;q=0.9")
	req.Header.Set("x-locale", xlocale.String())
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
	req.Header.Set("Origin", "https://www.lego.com")
	req.Header.Set("Referer", "https://www.lego.com/en-us/pick-and-build/pick-a-brick")

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

	var graphQLResp graphQLResponse
	if err := json.Unmarshal(bodyBytes, &graphQLResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(graphQLResp.Errors) > 0 {
		return nil, fmt.Errorf("GraphQL error: %s", graphQLResp.Errors[0].Message)
	}

	// Check if no bricks were found
	if len(graphQLResp.Data.Elements) == 0 {
		zap.L().Debug("No Pick-a-Brick elements found for design ID",
			zap.String("design_id", designID),
		)
		return nil, ErrBrickNotFound
	}

	zap.L().Debug("Successfully fetched Pick-a-Brick elements",
		zap.String("design_id", designID),
		zap.Int("count", len(graphQLResp.Data.Elements)),
	)

	return graphQLResp.Data.Elements, nil
}

// FetchBricksByBrickID fetches a specific brick by its element ID (brick ID)
func (c *Client) FetchBricksByBrickID(brickID string, lang, xlocale language.Tag) ([]Brick, error) {

	zap.L().Debug("Fetching Pick-a-Brick element by brick ID",
		zap.String("brick_id", brickID),
		zap.String("lang", lang.String()),
		zap.String("xlocale", xlocale.String()),
	)

	// GraphQL query from the LEGO API
	query := `query PickABrickQuery($input: ElementQueryInput!, $sku: String) {
  searchElements(input: $input) {
    results {
      ...ElementLeaf
      __typename
    }
    facets {
      ...FacetData
      __typename
    }
    set {
      id
      type
      name
      imageUrl
      instructionsUrl
      pieces
      inStock
      price {
        formattedAmount
        __typename
      }
      __typename
    }
    total
    count
    __typename
  }
}

fragment FacetData on Facet {
  id
  key
  name
  labels {
    count
    key
    name
    children {
      count
      key
      name
      ... on FacetValue {
        value
        __typename
      }
      __typename
    }
    ... on FacetValue {
      value
      __typename
    }
    ... on FacetRange {
      from
      to
      __typename
    }
    __typename
  }
  __typename
}

fragment ElementLeaf on SearchResultElement {
  id
  designId
  collapseDesignId
  name
  imageUrl
  maxOrderQuantity
  deliveryChannel
  colorHex
  contrastColorHex
  price {
    centAmount
    formattedAmount
    currencyCode
    formattedValue
    __typename
  }
  quantityInSet(sku: $sku)
  facets {
    category {
      ...ElementFacetCategory
      __typename
    }
    subcategory {
      ...ElementFacetCategory
      __typename
    }
    color {
      ...ElementFacetCategory
      __typename
    }
    colorFamily {
      ...ElementFacetCategory
      __typename
    }
    system
    __typename
  }
  siblings {
    id
    colorHex
    contrastColorHex
    availability
    price {
      formattedAmount
      formattedValue
      __typename
    }
    __typename
  }
  availability
  __typename
}

fragment ElementFacetCategory on ElementCategory {
  name
  key
  __typename
}`

	// Build the GraphQL request
	reqBody := graphQLRequest{
		OperationName: "PickABrickQuery",
		Variables: map[string]interface{}{
			"input": map[string]interface{}{
				"page":    1,
				"perPage": 50,
				"sort": map[string]interface{}{
					"key":       "RELEVANCE",
					"direction": "DESC",
				},
				"availability":  []string{"AVAILABLE", "OUT_OF_STOCK"},
				"query":         brickID,
				"fetchSiblings": true,
			},
		},
		Query: query,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	apiURL := "https://www.lego.com/api/graphql/PickABrickQuery"
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set required headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", lang.String()+",en;q=0.9")
	req.Header.Set("x-locale", xlocale.String())
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
	req.Header.Set("Origin", "https://www.lego.com")
	req.Header.Set("Referer", "https://www.lego.com/en-us/pick-and-build/pick-a-brick")

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

	var graphQLResp graphQLSearchResponse
	if err := json.Unmarshal(bodyBytes, &graphQLResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(graphQLResp.Errors) > 0 {
		return nil, fmt.Errorf("GraphQL error: %s", graphQLResp.Errors[0].Message)
	}

	// Check if no bricks were found
	if len(graphQLResp.Data.SearchElements.Results) == 0 {
		zap.L().Debug("No Pick-a-Brick element found for brick ID",
			zap.String("brick_id", brickID),
		)
		return nil, ErrBrickNotFound
	}

	zap.L().Debug("Successfully fetched Pick-a-Brick element",
		zap.String("brick_id", brickID),
		zap.Int("count", len(graphQLResp.Data.SearchElements.Results)),
	)

	return graphQLResp.Data.SearchElements.Results, nil
}
