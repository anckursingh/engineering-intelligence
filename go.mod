module github.com/anckursingh/engineering-intelligence

go 1.26.0

require (
	github.com/ancku/aikoql-sdk v0.0.0-00010101000000-000000000000
	github.com/google/go-github/v92 v92.0.0
)

require github.com/google/go-querystring v1.2.0 // indirect

// The aikoql Go SDK lives in the Mnemosyne repo (parallel development);
// consumed via replace until it is published as a module.
replace github.com/ancku/aikoql-sdk => ../Mnemosyne/crates/sdk/go
