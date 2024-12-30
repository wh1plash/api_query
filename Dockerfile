FROM golang:1.23

WORKDIR /app

COPY  go.* ./

RUN go mod download

COPY . .

RUN make build

EXPOSE 8080

CMD ["./bin/server"]