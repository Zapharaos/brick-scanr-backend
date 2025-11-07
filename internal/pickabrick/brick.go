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

type Price struct {
	CentAmount      int     `json:"centAmount"`
	CurrencyCode    string  `json:"currencyCode"`
	FormattedAmount string  `json:"formattedAmount"`
	FormattedValue  float64 `json:"formattedValue"`
}

type Brick struct {
	ID               string `json:"id"`
	DesignID         string `json:"designId"`
	CollapseDesignID string `json:"collapseDesignId"`
	Name             string `json:"name"`
	ColorHex         string `json:"colorHex"`
	ContrastColorHex string `json:"contrastColorHex"`
	Price            Price  `json:"price"`
	Availability     string `json:"availability"`
	MaxOrderQuantity int    `json:"maxOrderQuantity"`
	DeliveryChannel  string `json:"deliveryChannel"`
}

type graphQLRequest struct {
	OperationName string                 `json:"operationName"`
	Variables     map[string]interface{} `json:"variables"`
	Query         string                 `json:"query"`
}

type graphQLResponse struct {
	Data struct {
		Elements []Brick `json:"elements"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors,omitempty"`
}

// FetchBricksByDesignID fetches all bricks matching the designID
func (c *Client) FetchBricksByDesignID(designID string, locale language.Tag, currency language.Tag) ([]Brick, error) {
	zap.L().Debug("Fetching Pick-a-Brick elements by design ID",
		zap.String("design_id", designID),
		zap.String("locale", locale.String()),
		zap.String("currency", currency.String()),
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
	req.Header.Set("Accept-Language", locale.String()+",en;q=0.9")
	req.Header.Set("x-locale", currency.String())
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36")
	req.Header.Set("Origin", "https://www.lego.com")
	req.Header.Set("Referer", "https://www.lego.com/en-us/pick-and-build/pick-a-brick")

	// Execute the request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch data: %w", err)
	}
	defer resp.Body.Close()

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

	zap.L().Debug("Successfully fetched Pick-a-Brick elements",
		zap.String("design_id", designID),
		zap.Int("count", len(graphQLResp.Data.Elements)),
	)

	return graphQLResp.Data.Elements, nil
}
