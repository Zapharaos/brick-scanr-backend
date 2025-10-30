package bricklink

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
)

func TestSearchSets(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	zap.ReplaceGlobals(logger)
	defer logger.Sync()

	client := NewClient()
	query := "21043"

	sets, err := client.SearchSets(query)
	if err != nil {
		t.Fatalf("Failed to search sets: %v", err)
	}

	if len(sets) == 0 {
		t.Error("Expected at least one set in search results")
	}

	t.Logf("Found %d sets matching query '%s'", len(sets), query)

	if len(sets) > 0 {
		set := sets[0]
		t.Logf("First set: ID=%d, ItemNo=%s, Name=%s", set.IDItem, set.StrItemNo, set.StrItemName)

		if set.IDItem == 0 {
			t.Error("Expected IDItem to be non-zero")
		}
		if set.StrItemNo == "" {
			t.Error("Expected StrItemNo to be non-empty")
		}
		if set.StrItemName == "" {
			t.Error("Expected StrItemName to be non-empty")
		}
		if set.TypeItem != "S" {
			t.Errorf("Expected TypeItem to be 'S', got '%s'", set.TypeItem)
		}
	}
}

func TestFetchInventory(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	zap.ReplaceGlobals(logger)
	defer logger.Sync()

	// Create temp file for debugging
	tempFile := filepath.Join(os.TempDir(), "bricklink_test_output.html")
	defer os.Remove(tempFile)

	client := NewClient()
	itemID := "171510"
	setNumber := "21043-1"

	inventory, err := client.FetchInventory(itemID, setNumber)
	if err != nil {
		t.Fatalf("Failed to fetch inventory: %v", err)
	}

	if inventory == nil {
		t.Fatal("Expected inventory to be non-nil")
	}

	if inventory.SetNumber != setNumber {
		t.Errorf("Expected set number %s, got %s", setNumber, inventory.SetNumber)
	}

	t.Logf("Found %d items in inventory", len(inventory.Items))

	if len(inventory.Items) == 0 {
		t.Error("Expected at least one item in the inventory")
	}

	if len(inventory.Items) > 0 {
		item := inventory.Items[0]
		t.Logf("First item: ItemNo=%s, Qty=%s, Desc=%s", item.ItemNo, item.Quantity, item.Description)
	}
}

func TestCompleteFlow(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	zap.ReplaceGlobals(logger)
	defer logger.Sync()

	client := NewClient()

	// Step 1: Search
	query := "21043"
	sets, err := client.SearchSets(query)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(sets) == 0 {
		t.Fatal("No sets found")
	}

	// Step 2: Fetch inventory
	selectedSet := sets[0]
	inventory, err := client.FetchInventory(
		fmt.Sprintf("%d", selectedSet.IDItem),
		selectedSet.StrItemNo,
	)
	if err != nil {
		t.Fatalf("Inventory fetch failed: %v", err)
	}

	t.Logf("Complete flow: Found set %s with %d parts",
		selectedSet.StrItemName, len(inventory.Items))

	if len(inventory.Items) == 0 {
		t.Error("Expected parts in inventory")
	}
}
