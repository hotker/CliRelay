package apitools

type APICallRequest struct {
	AuthIndexSnake  *string           `json:"auth_index"`
	AuthIndexCamel  *string           `json:"authIndex"`
	AuthIndexPascal *string           `json:"AuthIndex"`
	Method          string            `json:"method"`
	URL             string            `json:"url"`
	Header          map[string]string `json:"header"`
	Data            string            `json:"data"`
}

type APICallResponse struct {
	StatusCode int                 `json:"status_code"`
	Header     map[string][]string `json:"header"`
	Body       string              `json:"body"`
}
