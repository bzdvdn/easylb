FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/controller ./cmd/controller && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/speaker ./cmd/speaker

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/controller /controller
COPY --from=build /out/speaker /speaker
USER 65532:65532
ENTRYPOINT ["/controller"]
