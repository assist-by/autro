FROM golang:1.22-alpine

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY consumer.go .

RUN go build -o consumer

CMD ["./consumer"]