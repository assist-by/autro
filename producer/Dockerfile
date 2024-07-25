FROM golang:1.22-alpine

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY producer.go .

RUN go build -o producer

CMD ["./producer"]