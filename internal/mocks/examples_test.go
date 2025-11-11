package mocks_test

import (
	"encoding/json"
	"testing"

	"github.com/Zapharaos/brick-scanr-backend/internal/mocks"
)

// TestExampleBricklinkSearchMock demonstrates how to use BrickLink search mock
func TestExampleBricklinkSearchMock(t *testing.T) {
	// Load the mock data
	data, err := mocks.LoadBricklinkSearchMock("21043")
	if err != nil {
		t.Fatalf("Failed to load mock: %v", err)
	}

	// Parse the response
	var response struct {
		Result struct {
			TypeList []struct {
				Type  string `json:"type"`
				Items []struct {
					StrItemNo   string `json:"strItemNo"`
					StrItemName string `json:"strItemName"`
				} `json:"items"`
			} `json:"typeList"`
		} `json:"result"`
	}

	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	// Verify the data
	if len(response.Result.TypeList) == 0 {
		t.Fatal("No type lists found")
	}

	// Find the Sets type list
	for _, typeList := range response.Result.TypeList {
		if typeList.Type == "S" && len(typeList.Items) > 0 {
			item := typeList.Items[0]
			t.Logf("Found set: %s - %s", item.StrItemNo, item.StrItemName)

			if item.StrItemNo != "21043-1" {
				t.Errorf("Expected set 21043-1, got %s", item.StrItemNo)
			}
			if item.StrItemName != "San Francisco" {
				t.Errorf("Expected 'San Francisco', got %s", item.StrItemName)
			}
			return
		}
	}

	t.Fatal("No sets found in mock data")
}

// TestExamplePickabrickElementsMock demonstrates how to use Pick-a-Brick mock
func TestExamplePickabrickElementsMock(t *testing.T) {
	// Load the mock data
	data, err := mocks.LoadPickabrickElementsByDesignMock("3003")
	if err != nil {
		t.Fatalf("Failed to load mock: %v", err)
	}

	// Parse the GraphQL response
	var response struct {
		Data struct {
			Elements []struct {
				ID       string `json:"id"`
				DesignID string `json:"designId"`
				Name     string `json:"name"`
				ColorHex string `json:"colorHex"`
				Price    struct {
					CentAmount      int     `json:"centAmount"`
					FormattedAmount string  `json:"formattedAmount"`
					FormattedValue  float64 `json:"formattedValue"`
				} `json:"price"`
			} `json:"elements"`
		} `json:"data"`
	}

	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	// Verify we have elements
	if len(response.Data.Elements) == 0 {
		t.Fatal("No elements found")
	}

	// Check first element
	elem := response.Data.Elements[0]
	t.Logf("Found brick: %s - %s (€%.2f)", elem.DesignID, elem.Name, elem.Price.FormattedValue)

	if elem.DesignID != "3003" {
		t.Errorf("Expected design ID 3003, got %s", elem.DesignID)
	}
	if elem.Name != "Brick 2 x 2" {
		t.Errorf("Expected 'Brick 2 x 2', got %s", elem.Name)
	}
}

// TestExampleBricklinkInventoryMock demonstrates how to parse inventory HTML
func TestExampleBricklinkInventoryMock(t *testing.T) {
	// Load the mock HTML
	html, err := mocks.LoadBricklinkInventoryMock("21043")
	if err != nil {
		t.Fatalf("Failed to load mock: %v", err)
	}

	// Simple check - verify it contains expected elements
	if len(html) == 0 {
		t.Fatal("HTML is empty")
	}

	// Check for expected table class
	if !containsString(html, "pciinvItemRow") {
		t.Error("Expected inventory table class not found")
	}

	// Check for some expected parts
	expectedParts := []string{"3003", "3022", "3023", "3024", "3069b"}
	for _, part := range expectedParts {
		if containsString(html, part) {
			t.Logf("✓ Found part: %s", part)
		}
	}
}

func containsString(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 &&
		(s == substr || len(s) >= len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
