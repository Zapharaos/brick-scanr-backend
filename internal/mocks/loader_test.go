package mocks

import (
	"testing"
)

func TestLoadBricklinkSearchMock(t *testing.T) {
	data, err := LoadBricklinkSearchMock("21043")
	if err != nil {
		t.Fatalf("Failed to load mock: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("Mock data is empty")
	}
	t.Logf("Successfully loaded %d bytes of mock data", len(data))
}

func TestLoadBricklinkInventoryMock(t *testing.T) {
	html, err := LoadBricklinkInventoryMock("21043")
	if err != nil {
		t.Fatalf("Failed to load mock: %v", err)
	}
	if len(html) == 0 {
		t.Fatal("Mock HTML is empty")
	}
	t.Logf("Successfully loaded %d bytes of mock HTML", len(html))
}

func TestLoadPickabrickElementsByDesignMock(t *testing.T) {
	data, err := LoadPickabrickElementsByDesignMock("3003")
	if err != nil {
		t.Fatalf("Failed to load mock: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("Mock data is empty")
	}
	t.Logf("Successfully loaded %d bytes of mock data", len(data))
}

func TestLoadPickabrickSearchByBrickMock(t *testing.T) {
	data, err := LoadPickabrickSearchByBrickMock("6225933")
	if err != nil {
		t.Fatalf("Failed to load mock: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("Mock data is empty")
	}
	t.Logf("Successfully loaded %d bytes of mock data", len(data))
}

func TestLoadNonExistentMock(t *testing.T) {
	_, err := LoadBricklinkSearchMock("99999")
	if err == nil {
		t.Fatal("Expected error for non-existent mock, got nil")
	}
	t.Logf("Correctly returned error: %v", err)
}

func TestLoadLegoProductDetailsMock(t *testing.T) {
	data, err := LoadLegoProductDetailsMock("san-francisco-21043")
	if err != nil {
		t.Fatalf("Failed to load mock: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("Mock data is empty")
	}
	t.Logf("Successfully loaded %d bytes of mock data", len(data))
}
