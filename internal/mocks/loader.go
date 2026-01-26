package mocks

import (
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed bricklink/*.json bricklink/*.html pickabrick/*.json lego/*.json
var mockFiles embed.FS

// LoadBricklinkSearchMock loads a BrickLink search mock response
func LoadBricklinkSearchMock(setNumber string) ([]byte, error) {
	filename := fmt.Sprintf("bricklink/search_%s.json", setNumber)
	data, err := mockFiles.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to load mock file %s: %w", filename, err)
	}
	return data, nil
}

// LoadBricklinkInventoryMock loads a BrickLink inventory mock response (HTML)
func LoadBricklinkInventoryMock(setNumber string) (string, error) {
	filename := fmt.Sprintf("bricklink/inventory_%s.html", setNumber)
	data, err := mockFiles.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("failed to load mock file %s: %w", filename, err)
	}
	return string(data), nil
}

// LoadBricklinkSetDetailsMock loads a BrickLink set details mock response
func LoadBricklinkSetDetailsMock(itemID string) ([]byte, error) {
	filename := fmt.Sprintf("bricklink/set_details_%s.json", itemID)
	data, err := mockFiles.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to load mock file %s: %w", filename, err)
	}
	return data, nil
}

// LoadPickabrickElementsByDesignMock loads a Pick-a-Brick elements by design ID mock response
func LoadPickabrickElementsByDesignMock(designID string) ([]byte, error) {
	filename := fmt.Sprintf("pickabrick/elements_by_design_%s.json", designID)
	data, err := mockFiles.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to load mock file %s: %w", filename, err)
	}
	return data, nil
}

// LoadPickabrickSearchByBrickMock loads a Pick-a-Brick search by brick ID mock response
func LoadPickabrickSearchByBrickMock(brickID string) ([]byte, error) {
	filename := fmt.Sprintf("pickabrick/search_by_brick_%s.json", brickID)
	data, err := mockFiles.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to load mock file %s: %w", filename, err)
	}
	return data, nil
}

// LoadLegoProductDetailsMock loads a LEGO product details mock response
func LoadLegoProductDetailsMock(slug string) ([]byte, error) {
	filename := fmt.Sprintf("lego/product_%s.json", slug)
	data, err := mockFiles.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to load mock file %s: %w", filename, err)
	}
	return data, nil
}

// UnmarshalJSON is a helper to unmarshal JSON data into a target
func UnmarshalJSON(data []byte, target interface{}) error {
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	return nil
}
