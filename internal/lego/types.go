package lego

// ProductDetailsResponse represents the response from the ProductDetails GraphQL endpoint
type ProductDetailsResponse struct {
	Data ProductDetailsData `json:"data"`
}

type ProductDetailsData struct {
	Product Product `json:"product"`
}

type Product struct {
	ID              string          `json:"id"`
	ProductCode     string          `json:"productCode"`
	Name            string          `json:"name"`
	Slug            string          `json:"slug"`
	MetaTitle       string          `json:"metaTitle"`
	MetaDescription string          `json:"metaDescription"`
	Variant         ProductVariant  `json:"variant"`
	SocialImage     string          `json:"socialImage"`
	PrimaryImage    string          `json:"primaryImage"`
	BaseImgUrl      string          `json:"baseImgUrl"`
	Description     string          `json:"description"`
	FeaturesText    string          `json:"featuresText"`
	ProductMedia    ProductMediaSet `json:"productMedia"`
}

type ProductVariant struct {
	ID         string            `json:"id"`
	SKU        string            `json:"sku"`
	VIPPoints  int               `json:"vipPoints"`
	Price      Price             `json:"price"`
	ListPrice  Price             `json:"listPrice"`
	Attributes ProductAttributes `json:"attributes"`
}

type Price struct {
	CentAmount      int     `json:"centAmount"`
	FormattedAmount string  `json:"formattedAmount"`
	FormattedValue  float64 `json:"formattedValue"`
	CurrencyCode    string  `json:"currencyCode"`
}

type ProductAttributes struct {
	AvailabilityStatus string  `json:"availabilityStatus"`
	AvailabilityText   string  `json:"availabilityText"`
	CanAddToBag        bool    `json:"canAddToBag"`
	AgeRange           string  `json:"ageRange"`
	PieceCount         int     `json:"pieceCount"`
	Rating             float64 `json:"rating"`
	IsNew              bool    `json:"isNew"`
	OnSale             bool    `json:"onSale"`
	MaxOrderQuantity   int     `json:"maxOrderQuantity"`
}

type ProductMediaSet struct {
	Items []ProductImage `json:"items"`
}

type ProductImage struct {
	ID         string            `json:"id"`
	BaseImgUrl string            `json:"baseImgUrl"`
	Sizes      ProductImageSizes `json:"sizes"`
}

type ProductImageSizes struct {
	Desktop ProductImageUrls `json:"desktop"`
	Mobile  ProductImageUrls `json:"mobile"`
	Tablet  ProductImageUrls `json:"tablet"`
}

type ProductImageUrls struct {
	URL           string `json:"url"`
	ThumbnailURL  string `json:"thumbnailUrl"`
	HighResURL    string `json:"highResUrl"`
	FullscreenURL string `json:"fullscreenUrl"`
}
