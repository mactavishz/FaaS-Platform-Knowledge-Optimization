module github.com/mactavishz/FaaS-Platform-Knowledge-Optimization

go 1.25.4

require (
	github.com/openfaas/go-sdk v0.0.0
	github.com/stretchr/testify v1.11.1
	k8s.io/apimachinery v0.34.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/drone/envsubst v1.0.3 // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/ryanuber/go-glob v1.0.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	sigs.k8s.io/json v0.0.0-20241014173422-cfa47c3a1cc8 // indirect
)

replace github.com/openfaas/go-sdk => ./go-sdk
