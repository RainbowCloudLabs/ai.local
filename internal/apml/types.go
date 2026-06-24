package apml

type APMLConfig struct {
	Title     string                    `yaml:"title"`
	BaseURI   string                    `yaml:"baseUri"`
	Version   string                    `yaml:"version"`
	Quotas    map[string]QuotaDetail    `yaml:"quotas"`
	Providers map[string]ProviderConfig `yaml:"providers"`
	Routes    map[string]RouteConfig    `yaml:",inline"`
}

type QuotaDetail struct {
	Mode    string `yaml:"mode"`
	Daily   int64  `yaml:"daily"`
	Monthly int64  `yaml:"monthly"`
}

type ProviderConfig struct {
	Host         string           `yaml:"host"`
	APIKeyPrefix string           `yaml:"api_key_prefix"`
	InputMessage string           `yaml:"input_message"`
	Usage        *UsageConfig     `yaml:"usage"`
	Streaming    *StreamingConfig `yaml:"streaming"`
}

type RouteConfig struct {
	Provider string `yaml:"provider"`
	Quota    string `yaml:"quota"`
}

type UsageConfig struct {
	InputTokens  string `yaml:"input_tokens"`
	OutputTokens string `yaml:"output_tokens"`
}

type StreamingConfig struct {
	Mode          string                 `yaml:"mode"`
	InputTokens   string                 `yaml:"input_tokens"`
	OutputTokens  string                 `yaml:"output_tokens"`
	Input         *EventConfig           `yaml:"input"`
	Output        *EventConfig           `yaml:"output"`
	RequestOption map[string]interface{} `yaml:"request_option"`
}

type EventConfig struct {
	ChunkType    string `yaml:"chunk_type"`
	InputTokens  string `yaml:"input_tokens"`
	OutputTokens string `yaml:"output_tokens"`
}
