FROM golang:1.16 AS builder

# Set the Current Working Directory inside the container
WORKDIR /app

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . .

RUN go test -v ./...

WORKDIR /app/cmd/api-pointofinterest

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build

FROM alpine

RUN apk add ca-certificates

COPY --from=builder /app/cmd/api-pointofinterest/api-pointofinterest /app/

ENTRYPOINT ["/app/api-pointofinterest"]
