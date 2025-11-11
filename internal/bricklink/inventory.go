package bricklink

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Zapharaos/brick-scanr-backend/internal/mocks"
	"go.uber.org/zap"
	"golang.org/x/net/html"
)

// FetchInventory fetches the inventory data for a given set number
func (c *Client) FetchInventory(itemID int, setNumber string) (*Inventory, error) {
	// If mock mode is enabled, load from mock file
	if c.useMocks {
		zap.L().Info("Using mock data for BrickLink inventory", zap.String("set_number", setNumber))

		htmlContent, err := mocks.LoadBricklinkInventoryMock(setNumber)
		if err != nil {
			return nil, fmt.Errorf("failed to load mock inventory data: %w", err)
		}

		inventory, err := c.parseInventory(htmlContent, setNumber)

		if err != nil {
			return nil, fmt.Errorf("failed to parse mock inventory: %w", err)
		}

		return inventory, nil
	}

	baseURL := "https://www.bricklink.com/v2/catalog/catalogitem_invtab.page"
	params := url.Values{}
	params.Add("idItem", fmt.Sprintf("%d", itemID))
	params.Add("st", "1")
	params.Add("show_invid", "0")
	params.Add("show_matchcolor", "1")
	params.Add("show_pglink", "0")
	params.Add("show_pcc", "1")
	params.Add("show_missingpcc", "1")
	params.Add("itemNoSeq", setNumber)

	requestURL := fmt.Sprintf("%s?%s", baseURL, params.Encode())

	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "text/html, */*; q=0.01")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://www.bricklink.com/")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	zap.L().Info("Fetching BrickLink inventory", zap.String("url", requestURL))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch data: %w", err)
	}
	defer resp.Body.Close()

	// Handle different HTTP status codes
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	inventory, err := c.parseInventory(string(body), setNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to parse inventory: %w", err)
	}

	return inventory, nil
}

func (c *Client) parseInventory(htmlContent, setNumber string) (*Inventory, error) {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	inventory := &Inventory{
		SetNumber: setNumber,
		Items:     []InventoryItem{},
		FetchedAt: time.Now(),
	}

	var findRows func(*html.Node)
	findRows = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			if hasClass(n, "pciinvItemRow") {
				item := c.parseItemRow(n)
				if item.ItemNo != "" {
					inventory.Items = append(inventory.Items, item)
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			findRows(child)
		}
	}

	findRows(doc)

	zap.L().Info("Parsed BrickLink inventory",
		zap.String("set_number", setNumber),
		zap.Int("item_count", len(inventory.Items)))
	return inventory, nil
}

func (c *Client) parseItemRow(row *html.Node) InventoryItem {
	item := InventoryItem{}
	colIndex := 0

	for td := row.FirstChild; td != nil; td = td.NextSibling {
		if td.Type != html.ElementNode || td.Data != "td" {
			continue
		}

		switch colIndex {
		case 1:
			item.ImageURL = extractImageURL(td)
		case 2:
			item.Quantity = strings.TrimSpace(getTextContent(td))
		case 3:
			item.ItemNo = extractItemNo(td)
		case 4:
			item.Description, item.Color, item.ItemIDs = extractDescription(td)
		}
		colIndex++
	}

	return item
}

func extractImageURL(td *html.Node) string {
	var findImg func(*html.Node) string
	findImg = func(n *html.Node) string {
		if n.Type == html.ElementNode && n.Data == "img" {
			for _, attr := range n.Attr {
				if attr.Key == "src" {
					if !strings.HasPrefix(attr.Val, "http") {
						return "https:" + attr.Val
					}
					return attr.Val
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if result := findImg(c); result != "" {
				return result
			}
		}
		return ""
	}
	return findImg(td)
}

func extractItemNo(td *html.Node) string {
	var findLink func(*html.Node) string
	findLink = func(n *html.Node) string {
		if n.Type == html.ElementNode && n.Data == "a" {
			return strings.TrimSpace(getTextContent(n))
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if result := findLink(c); result != "" {
				return result
			}
		}
		return ""
	}
	return findLink(td)
}

func extractDescription(td *html.Node) (string, string, []string) {
	var description, color string
	var itemIDs []string

	var traverse func(*html.Node)
	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "b" {
				fullDesc := strings.TrimSpace(getTextContent(n))
				description = fullDesc
				parts := strings.SplitN(fullDesc, " ", 2)
				if len(parts) > 1 {
					color = parts[0]
				}
			} else if n.Data == "span" && hasClass(n, "pciinvPartsColorCode") {
				itemIDstr := strings.TrimSpace(getTextContent(n))
				itemIDs = parseItemIDs(itemIDstr)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}

	traverse(td)
	return description, color, itemIDs
}

// parseItemIDs extracts item IDs from a string like "X or Y or Z"
func parseItemIDs(itemIDstr string) []string {
	if itemIDstr == "" {
		return []string{}
	}

	// Split by " or " to handle multiple IDs
	parts := strings.Split(itemIDstr, " or ")
	ids := make([]string, 0, len(parts))

	for _, part := range parts {
		id := strings.TrimSpace(part)
		if id != "" {
			ids = append(ids, id)
		}
	}

	return ids
}

func hasClass(n *html.Node, className string) bool {
	for _, attr := range n.Attr {
		if attr.Key == "class" && strings.Contains(attr.Val, className) {
			return true
		}
	}
	return false
}

func getTextContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var text strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		text.WriteString(getTextContent(c))
	}
	return text.String()
}
