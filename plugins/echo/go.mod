module github.com/Orfeo42/ai-orchestration/plugins/echo

go 1.27.3

require (
	github.com/Orfeo42/ai-orchestration/pluginapi v0.0.0
	github.com/stretchr/testify v1.12.1
)

require (
	github.com/google/jsonschema-go v0.4.3 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
)

replace github.com/Orfeo42/ai-orchestration/pluginapi => ../../pluginapi
