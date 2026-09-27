package api

// APIResponse define o envelope canônico de sucesso.
type APIResponse[T any] struct {
	Success bool  `json:"success"`
	Data    T     `json:"data"`
	Meta    *Meta `json:"meta,omitempty"`
}

// Meta define metadados de paginação ou contexto de consulta.
type Meta struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// APIErrorDetail descreve o erro interno ou validação de campo.
type APIErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// APIErrorResponse define o envelope de falha.
type APIErrorResponse struct {
	Success bool           `json:"success"`
	Error   APIErrorDetail `json:"error"`
}

// InvalidJSONError sinaliza sintaxe incorreta ao tratador global do Fiber.
type InvalidJSONError struct{}

func (InvalidJSONError) Error() string { return "Corpo JSON inválido." }
