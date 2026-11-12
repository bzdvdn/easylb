IMAGE ?= ghcr.io/bzdvdn/easylb:dev
GOLANGCI_LINT_VERSION ?= v2.15.0
GOSEC_VERSION ?= v2.30.0

.PHONY: build test vet fmt lint sec docker-build helm-lint helm-template e2e clean

build:
	CGO_ENABLED=0 go build -trimpath -o bin/controller ./cmd/controller
	CGO_ENABLED=0 go build -trimpath -o bin/speaker ./cmd/speaker

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w ./cmd ./internal ./api

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

sec:
	go run github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION) \
		-severity medium -confidence medium -exclude-generated ./...

docker-build:
	docker build -t $(IMAGE) .

helm-lint:
	helm lint charts/easylb

helm-template:
	helm template easylb charts/easylb --namespace easylb-system \
		-f charts/easylb/values-example.yaml --include-crds

e2e:
	./test/e2e/run.sh

clean:
	rm -rf bin
