package set

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/Zapharaos/brick-scanr-backend/internal/bricklink"
	"github.com/google/uuid"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

type FetchStatus int

const (
	FetchStatusPending FetchStatus = iota
	FetchStatusFetching
	FetchStatusCompleted
	FetchStatusFailed
)

type FetchErrorStep int

const (
	FetchErrorUnknown FetchErrorStep = iota + 1
	FetchErrorInitCache
	FetchErrorDetailsCache
	FetchErrorBatchCache
	FetchErrorFinalCache
	FetchErrorFetchInventory
	FetchErrorFetchPrices
)

type FetchError struct {
	Message string         `json:"message"`
	Step    FetchErrorStep `json:"step"`
}

// Set represents a Lego set. Allowed to be cached
type Set struct {
	// Fetching status and error
	FetchStatus FetchStatus `json:"fetch_status"`
	FetchError  *FetchError `json:"fetch_error,omitempty"`

	// Local data
	Id uuid.UUID `json:"id"`

	// General data
	Number string      `json:"number"`
	Slug   string      `json:"slug"`
	Prices PricePerTag `json:"prices"`

	// Could be made locale specific, but not for now
	Status          Status `json:"status"`
	Name            string `json:"name"`
	LegoURL         string `json:"lego_url"`
	InstructionsURL string `json:"instructions_url"`

	// Details from Bricklink
	Bricks          []BrickSet `json:"bricks"`
	Parts           int        `json:"parts"`
	ImageURL        string     `json:"image_url"`
	YearReleased    int        `json:"year_released"`
	BricklinkID     int        `json:"bricklink_id"`
	BricklinkNumber string     `json:"bricklink_number"`
}

// BuildLegoURL constructs the LEGO product URL based on the set's slug and the provided locale
func (s *Set) BuildLegoURL(xlocale language.Tag) {
	s.LegoURL = "https://www.lego.com/" + xlocale.String() + "/product/" + s.Slug
}

// BuildInstructionsURL constructs the LEGO building instructions URL based on the set's number and the provided locale
func (s *Set) BuildInstructionsURL(xlocale language.Tag) {
	s.InstructionsURL = "https://www.lego.com/" + xlocale.String() + "/service/building-instructions/" + s.Number
}

// GenerateSlug creates a URL-friendly slug for the set based on its name and number, with normalization and fallback logic
func (s *Set) GenerateSlug() {
	var number string

	// Handle set number: prefer explicit Number, otherwise extract first numeric part from BricklinkNumber (format: "<numbers>-<numbers>")
	if s.Number != "" {
		number = s.Number
	} else {
		raw := strings.TrimSpace(s.BricklinkNumber)
		// try to extract the first sequence of digits
		reNum := regexp.MustCompile(`\d+`)
		if m := reNum.FindString(raw); m != "" {
			number = m
		} else {
			// fallback: take substring before '-' if present, otherwise use the whole trimmed string
			if idx := strings.Index(raw, "-"); idx != -1 {
				number = raw[:idx]
			} else {
				number = raw
			}
		}
	}

	// Handle name: normalize (remove diacritics), lower-case, replace non-alphanumeric chars with '-' and trim/collapse dashes
	name := s.Name
	// Normalize to NFD to separate diacritics, then drop them
	name = norm.NFD.String(name)
	var b strings.Builder
	for _, r := range name {
		if unicode.Is(unicode.Mn, r) {
			// skip diacritic marks
			continue
		}
		b.WriteRune(r)
	}
	name = b.String()
	name = strings.ToLower(name)

	// Replace any sequence of characters that are not a-z or 0-9 with a single '-'
	re := regexp.MustCompile(`[^a-z0-9]+`)
	name = re.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")

	// Compose slug
	if number != "" && name != "" {
		s.Slug = name + "-" + number
	} else if number != "" {
		s.Slug = number
	} else {
		s.Slug = name
	}
}

// MapSetFromBricklinkSearch maps a Bricklink search item to an internal Set representation
func MapSetFromBricklinkSearch(bs bricklink.SearchItem) (Set, error) {
	// Assign a local UUID to each set
	setId, err := uuid.NewUUID()
	if err != nil {
		return Set{}, err
	}

	// Map to internal set representation
	return Set{
		Id:              setId,
		Name:            bs.StrItemName,
		BricklinkID:     bs.IDItem,
		BricklinkNumber: bs.StrItemNo,
	}, nil
}

// CalculateBricksTotalPrices calculates and applies the total price for each brick
// Returns the total sum and how many missing brick prices
func (s *Set) CalculateBricksTotalPrices() (int, int) {
	countMissingBrickPrices := 0
	sumTotalPriceCentAmount := 0

	// Process each brick
	for _, brick := range s.Bricks {

		// Brick reference is missing price
		if brick.Price.CentAmount == 0 {
			countMissingBrickPrices++
			continue
		}

		// Calculate total price for the brick
		brick.TotalPrice = brick.Price
		brick.TotalPrice.CentAmount = brick.Price.CentAmount * brick.Quantity
		sumTotalPriceCentAmount += brick.TotalPrice.CentAmount
	}

	return sumTotalPriceCentAmount, countMissingBrickPrices
}

// CleanupForCache performs cleanup on the Set before caching
func (s *Set) CleanupForCache() {
	for i := range s.Bricks {
		// ensure we call the pointer receiver on an addressable BrickSet
		_, _ = (&s.Bricks[i]).CleanupForCache()
	}
}
