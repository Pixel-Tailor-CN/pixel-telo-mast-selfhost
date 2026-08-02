FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY httpapi ./httpapi
COPY internal ./internal
COPY phone ./phone
COPY provider ./provider
COPY query ./query
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mast-selfhost ./cmd/mast-selfhost

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/mast-selfhost /mast-selfhost
EXPOSE 8443
ENTRYPOINT ["/mast-selfhost"]
