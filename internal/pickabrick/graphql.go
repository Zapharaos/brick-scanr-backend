package pickabrick

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

type graphQLSearchResponse struct {
	Data struct {
		SearchElements struct {
			Results []Brick `json:"results"`
		} `json:"searchElements"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors,omitempty"`
}
