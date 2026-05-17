FROM golang:1.24.2 AS build

WORKDIR /src

COPY go.mod ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/netq-exporter ./cmd/netq-exporter

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/netq-exporter /netq-exporter

EXPOSE 8080

ENTRYPOINT ["/netq-exporter"]
