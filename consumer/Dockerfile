FROM golang:1.22-alpine

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

# 전체 consumer 디렉토리 내용을 복사
COPY . .

RUN go build -o consumer

CMD ["./consumer"]