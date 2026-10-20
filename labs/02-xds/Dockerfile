FROM golang:1.25.6-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -o /out/control-plane .

FROM scratch
COPY --from=build /out/control-plane /control-plane
USER 65532:65532
ENTRYPOINT ["/control-plane"]
