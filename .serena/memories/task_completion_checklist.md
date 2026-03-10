# navilyrics — Task Completion Checklist

When finishing any coding task:

1. **Format**: `make fmt` (or `go fmt ./...`)
2. **Vet**: `make vet` (or `go vet ./...`)
3. **Test**: `make test` (or `go test -v -race ./...`)
4. **Build** (if touching cmd/): `make build`
5. **Commit**: one-line Conventional Commit, no body, no AI attribution
   - Format: `<type>[scope]: <description>`
   - Max 50 chars, lowercase imperative, no trailing period
